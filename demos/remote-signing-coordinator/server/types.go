package main

import "time"

type operation string

const (
	operationCreateAsset operation = "create_asset"
	operationIssueAsset  operation = "issue_asset"
)

type sessionStatus string

const (
	statusStaging            sessionStatus = "staging"
	statusWaitingSignature   sessionStatus = "waiting_signature"
	statusSignatureSubmitted sessionStatus = "signature_submitted"
	statusWaitingConfirm     sessionStatus = "waiting_confirmation"
	statusMining             sessionStatus = "mining"
	statusFinalized          sessionStatus = "finalized"
	statusFailed             sessionStatus = "failed"
)

type externalKey struct {
	XPub              string `json:"xpub"`
	MasterFingerprint string `json:"master_fingerprint"`
	DerivationPath    string `json:"derivation_path"`
}

type startSessionRequest struct {
	Operation    operation   `json:"operation"`
	Name         string      `json:"name"`
	AssetRef     string      `json:"asset_ref"`
	Amount       uint64      `json:"amount"`
	FeeRateSatKw uint32      `json:"fee_rate_sat_kw"`
	ExternalKey  externalKey `json:"external_key"`
}

type signingRequest struct {
	Operation      string      `json:"operation"`
	Statement      string      `json:"statement"`
	AssetRef       string      `json:"asset_ref"`
	IssuanceRef    string      `json:"issuance_ref,omitempty"`
	Name           string      `json:"name"`
	Amount         uint64      `json:"amount"`
	AssetType      string      `json:"asset_type"`
	ScriptKey      string      `json:"script_key"`
	AnchorOutpoint string      `json:"anchor_outpoint"`
	ExternalKey    externalKey `json:"external_key"`
	VirtualPSBT    string      `json:"virtual_psbt"`
}

type submitSignatureRequest struct {
	SignedVirtualPSBT string `json:"signed_virtual_psbt"`
}

type sessionResult struct {
	AssetRef    string `json:"asset_ref"`
	IssuanceRef string `json:"issuance_ref,omitempty"`
	Name        string `json:"name"`
	Amount      uint64 `json:"amount"`
}

type session struct {
	ID            string          `json:"id"`
	Operation     operation       `json:"operation"`
	Status        sessionStatus   `json:"status"`
	Request       *signingRequest `json:"request,omitempty"`
	Result        *sessionResult  `json:"result,omitempty"`
	BatchKey      string          `json:"batch_key,omitempty"`
	BatchState    string          `json:"batch_state,omitempty"`
	AnchorTxID    string          `json:"anchor_txid,omitempty"`
	MinedBlocks   int             `json:"mined_blocks,omitempty"`
	Error         string          `json:"error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	signedPSBT    chan string
	signatureSeen bool
}

func newSession(id string, op operation, now time.Time) *session {
	return &session{
		ID:         id,
		Operation:  op,
		Status:     statusStaging,
		CreatedAt:  now,
		UpdatedAt:  now,
		signedPSBT: make(chan string, 1),
	}
}

func (s *session) clone() *session {
	if s == nil {
		return nil
	}

	clone := *s
	clone.signedPSBT = nil

	return &clone
}
