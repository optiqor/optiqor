package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// ReceiptStore is the persistence seam. Production writes the receipts
// table; tests use a fake.
type ReceiptStore interface {
	Save(ctx context.Context, t tenancy.Context, id string, signed string, receipt receipts.Receipt) error
}

type ReceiptIssuePayload struct {
	Receipt receipts.Receipt `json:"receipt"`
}

type ReceiptIssue struct {
	Issuer *receipts.Issuer
	Store  ReceiptStore
}

func (ReceiptIssue) Name() string { return "receipt_issue" }

// Execute signs + persists. Store records both the canonical wire form
// and the parsed Receipt so the verification endpoint can return both
// without re-signing.
func (w ReceiptIssue) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	if w.Issuer == nil {
		return fmt.Errorf("receipt_issue: nil issuer")
	}
	if w.Store == nil {
		return fmt.Errorf("receipt_issue: nil store")
	}
	var p ReceiptIssuePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("receipt_issue: decode: %w", err)
	}
	r := p.Receipt
	if r.IssuedAtUTC.IsZero() {
		r.IssuedAtUTC = time.Now().UTC()
	}
	signed, err := w.Issuer.Sign(r)
	if err != nil {
		return fmt.Errorf("receipt_issue: sign: %w", err)
	}
	if err := w.Store.Save(ctx, t, r.ID, signed, r); err != nil {
		return fmt.Errorf("receipt_issue: save: %w", err)
	}
	return nil
}
