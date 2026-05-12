package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

const minManualFeeRateSatKw = 253

type issuer interface {
	CreateFungible(context.Context, tapsdk.FungibleAssetSpec,
		...tapsdk.MintOption) (*tapsdk.Asset, error)

	IssueFungible(context.Context, tapsdk.AssetRef, uint64,
		...tapsdk.MintOption) (*tapsdk.Issuance, error)
}

type coordinator struct {
	issuer issuer

	mu       sync.Mutex
	sessions map[string]*session
	now      func() time.Time
	newID    func() (string, error)
}

func newCoordinator(issuer issuer) *coordinator {
	return &coordinator{
		issuer:   issuer,
		sessions: make(map[string]*session),
		now:      time.Now,
		newID:    randomID,
	}
}

func (c *coordinator) startSession(_ context.Context,
	req startSessionRequest) (*session, error) {

	if c == nil || c.issuer == nil {
		return nil, errors.New("issuer is not configured")
	}

	externalKey, err := parseExternalKey(req.ExternalKey)
	if err != nil {
		return nil, err
	}
	if req.Amount == 0 {
		return nil, tapsdk.ErrZeroAmount
	}
	if req.FeeRateSatKw > 0 && req.FeeRateSatKw < minManualFeeRateSatKw {
		return nil, fmt.Errorf(
			"fee rate must be at least %d sat/kWU, about %.2f sat/vB",
			minManualFeeRateSatKw, float64(minManualFeeRateSatKw)/250,
		)
	}

	var assetRef tapsdk.AssetRef
	switch req.Operation {
	case operationCreateAsset:
		if strings.TrimSpace(req.Name) == "" {
			return nil, tapsdk.ErrAssetNameRequired
		}

	case operationIssueAsset:
		assetRef, err = tapsdk.ParseAssetRef(req.AssetRef)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unsupported operation: %s",
			req.Operation)
	}

	id, err := c.newID()
	if err != nil {
		return nil, err
	}

	session := newSession(id, req.Operation, c.now())

	c.mu.Lock()
	c.sessions[id] = session
	c.mu.Unlock()

	go c.runSession(
		context.Background(), id, req, assetRef, externalKey,
	)

	return session.clone(), nil
}

func (c *coordinator) listSessions() []*session {
	c.mu.Lock()
	defer c.mu.Unlock()

	sessions := make([]*session, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session.clone())
	}

	return sessions
}

func (c *coordinator) getSession(id string) (*session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session, ok := c.sessions[id]
	if !ok {
		return nil, false
	}

	return session.clone(), true
}

func (c *coordinator) submitSignature(id string,
	signedPSBT string) (*session, error) {

	signedPSBT = strings.TrimSpace(signedPSBT)
	if signedPSBT == "" {
		return nil, tapsdk.ErrExternalIssuanceSignatureRequired
	}

	c.mu.Lock()
	session, ok := c.sessions[id]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("session %s not found", id)
	}
	if session.Status != statusWaitingSignature {
		c.mu.Unlock()
		return nil, fmt.Errorf("session %s is not waiting for a signature",
			id)
	}
	if session.signatureSeen {
		c.mu.Unlock()
		return nil, fmt.Errorf("session %s already has a signature", id)
	}

	session.Status = statusSignatureSubmitted
	session.signatureSeen = true
	session.UpdatedAt = c.now()
	signed := session.signedPSBT
	clone := session.clone()
	c.mu.Unlock()

	select {
	case signed <- signedPSBT:
		return clone, nil

	default:
		return nil, fmt.Errorf("session %s signer is not ready", id)
	}
}

func (c *coordinator) runSession(ctx context.Context, id string,
	req startSessionRequest, assetRef tapsdk.AssetRef,
	externalKey tapsdk.ExternalKey) {

	signer := sessionSigner{
		coordinator: c,
		sessionID:   id,
	}
	opts := []tapsdk.MintOption{
		tapsdk.WithExternalIssuanceKey(externalKey),
		tapsdk.WithExternalIssuanceSigner(signer),
	}
	if req.FeeRateSatKw != 0 {
		opts = append(opts, tapsdk.WithMintFeeRate(req.FeeRateSatKw))
	}

	switch req.Operation {
	case operationCreateAsset:
		asset, err := c.issuer.CreateFungible(
			ctx, tapsdk.FungibleAssetSpec{
				Name:   strings.TrimSpace(req.Name),
				Amount: req.Amount,
			}, opts...,
		)
		if err != nil {
			c.failSession(id, err)
			return
		}

		c.completeSession(id, sessionResult{
			AssetRef: asset.AssetRef.String(),
			Name:     asset.Name,
			Amount:   asset.Amount,
		})

	case operationIssueAsset:
		issuance, err := c.issuer.IssueFungible(
			ctx, assetRef, req.Amount, opts...,
		)
		if err != nil {
			c.failSession(id, err)
			return
		}

		c.completeSession(id, sessionResult{
			AssetRef:    issuance.AssetRef.String(),
			IssuanceRef: issuance.Ref().String(),
			Name:        issuance.Name,
			Amount:      issuance.Amount,
		})
	}
}

func (c *coordinator) awaitSignature(ctx context.Context, id string,
	req tapsdk.IssuanceSigningRequest) (tapsdk.SignedIssuance, error) {

	c.mu.Lock()
	session, ok := c.sessions[id]
	if !ok {
		c.mu.Unlock()
		return tapsdk.SignedIssuance{}, fmt.Errorf(
			"session %s not found", id,
		)
	}

	session.Status = statusWaitingSignature
	session.Request = signingRequestFromSDK(req)
	session.UpdatedAt = c.now()
	signed := session.signedPSBT
	c.mu.Unlock()

	select {
	case signedPSBT := <-signed:
		return tapsdk.SignedIssuance{VirtualPSBT: signedPSBT}, nil

	case <-ctx.Done():
		return tapsdk.SignedIssuance{}, ctx.Err()
	}
}

func (c *coordinator) completeSession(id string, result sessionResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session, ok := c.sessions[id]
	if !ok {
		return
	}

	session.Status = statusFinalized
	session.Result = &result
	session.UpdatedAt = c.now()
}

func (c *coordinator) failSession(id string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session, ok := c.sessions[id]
	if !ok {
		return
	}

	session.Status = statusFailed
	session.Error = err.Error()
	session.UpdatedAt = c.now()
}

type sessionSigner struct {
	coordinator *coordinator
	sessionID   string
}

func (s sessionSigner) SignIssuance(ctx context.Context,
	req tapsdk.IssuanceSigningRequest) (tapsdk.SignedIssuance, error) {

	return s.coordinator.awaitSignature(ctx, s.sessionID, req)
}

func parseExternalKey(key externalKey) (tapsdk.ExternalKey, error) {
	fingerprint, err := hex.DecodeString(
		strings.TrimSpace(key.MasterFingerprint),
	)
	if err != nil {
		return tapsdk.ExternalKey{}, fmt.Errorf(
			"invalid master fingerprint: %w", err,
		)
	}
	if len(fingerprint) != 4 {
		return tapsdk.ExternalKey{}, fmt.Errorf(
			"master fingerprint must be 4 bytes",
		)
	}
	if strings.TrimSpace(key.XPub) == "" {
		return tapsdk.ExternalKey{}, errors.New("xpub is required")
	}
	if strings.TrimSpace(key.DerivationPath) == "" {
		return tapsdk.ExternalKey{}, errors.New(
			"derivation path is required",
		)
	}

	externalKey := tapsdk.ExternalKey{
		XPub:           strings.TrimSpace(key.XPub),
		DerivationPath: strings.TrimSpace(key.DerivationPath),
	}
	copy(externalKey.MasterFingerprint[:], fingerprint)

	return externalKey, nil
}

func signingRequestFromSDK(req tapsdk.IssuanceSigningRequest) *signingRequest {
	result := &signingRequest{
		Operation:   req.Operation.String(),
		Statement:   statementFor(req.Operation),
		AssetRef:    req.AssetRef.String(),
		Name:        req.Name,
		Amount:      req.Amount,
		AssetType:   assetTypeLabel(req.AssetType),
		ExternalKey: externalKeyFromSDK(req.ExternalKey),
		VirtualPSBT: req.VirtualPSBT,
	}

	if req.ScriptKey != nil {
		result.ScriptKey = req.ScriptKey.PubKey.String()
	}
	if req.AnchorGenesis != nil {
		result.AnchorOutpoint = req.AnchorGenesis.GenesisPoint
	}

	return result
}

func externalKeyFromSDK(key tapsdk.ExternalKey) externalKey {
	return externalKey{
		XPub:              key.XPub,
		MasterFingerprint: hex.EncodeToString(key.MasterFingerprint[:]),
		DerivationPath:    key.DerivationPath,
	}
}

func statementFor(op tapsdk.IssuanceOperation) string {
	switch op {
	case tapsdk.IssuanceOperationCreateAsset:
		return "This virtual PSBT authorizes the first Issuance " +
			"for this Asset."

	case tapsdk.IssuanceOperationIssueAsset:
		return "This virtual PSBT authorizes a new Issuance " +
			"for this Asset."

	case tapsdk.IssuanceOperationCreateCollection:
		return "This virtual PSBT authorizes the first item in " +
			"this Collection."

	case tapsdk.IssuanceOperationMintCollectionItem:
		return "This virtual PSBT authorizes a new item in this " +
			"Collection."

	default:
		return "This virtual PSBT authorizes a Taproot Assets " +
			"Issuance."
	}
}

func assetTypeLabel(assetType tapsdk.AssetType) string {
	switch assetType {
	case tapsdk.AssetTypeFungible:
		return "Asset"
	case tapsdk.AssetTypeNFT:
		return "Collection item"
	default:
		return "Unknown"
	}
}

func randomID() (string, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(id[:]), nil
}
