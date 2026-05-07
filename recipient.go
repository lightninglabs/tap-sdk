package tapsdk

import (
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
)

func normaliseSendRecipients(recipients []entities.Recipient,
	forceExplicit bool) ([]entities.Recipient, error) {

	if len(recipients) == 0 {
		return nil, ErrNoRecipients
	}

	decoded := make([]*entities.Address, len(recipients))
	anyExplicit := false
	for i, recipient := range recipients {
		addr, err := entities.DecodeAddress(recipient.Address)
		if err != nil {
			return nil, err
		}

		decoded[i] = addr
		_, hasAmount := recipient.Amount()
		anyExplicit = anyExplicit || hasAmount
	}

	if err := validateSingleAssetSendBatch(decoded); err != nil {
		return nil, err
	}

	useExplicitAmounts := forceExplicit || anyExplicit
	normalised := make([]entities.Recipient, len(recipients))
	for i, recipient := range recipients {
		amount, hasAmount := recipient.Amount()
		if hasAmount {
			if amount == 0 {
				return nil, ErrZeroAmount
			}

			if decoded[i].Amount > 0 && decoded[i].Amount != amount {
				return nil, fmt.Errorf(
					"%w: address embeds %d, caller passed %d",
					ErrAmountMismatch, decoded[i].Amount,
					amount,
				)
			}

			normalised[i] = entities.RecipientWithAmount(
				recipient.Address, amount,
			)
			continue
		}

		if decoded[i].Amount == 0 {
			return nil, ErrAmountRequired
		}

		normalised[i] = entities.RecipientWithEmbeddedAmount(
			recipient.Address,
		)
		if useExplicitAmounts {
			normalised[i] = entities.RecipientWithAmount(
				recipient.Address, decoded[i].Amount,
			)
		}
	}

	return normalised, nil
}
