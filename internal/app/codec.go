package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// The DTOs below are the persisted schema (v1). They live in the
// application layer so the domain never sees serialization concerns.

type accountDTO struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Currency string    `json:"currency"`
	Policy   string    `json:"policy"`
	Limit    int64     `json:"limit,omitempty"`
	OpenedAt time.Time `json:"opened_at"`
}

type lineDTO struct {
	Account  string `json:"account"`
	Side     string `json:"side"`
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type entryDTO struct {
	ID         string    `json:"id"`
	PostedAt   time.Time `json:"posted_at"`
	Memo       string    `json:"memo,omitempty"`
	ReversalOf string    `json:"reversal_of,omitempty"`
	Lines      []lineDTO `json:"lines"`
}

// Policy names used by commands and the persisted schema.
const (
	PolicyUnrestricted = "unrestricted"
	PolicyNoOverdraft  = "no-overdraft"
	PolicyLimit        = "overdraft-limit"
)

// ParsePolicy maps a policy name (+limit) to a domain policy.
func ParsePolicy(name string, limit int64) (domain.OverdraftPolicy, error) {
	switch name {
	case "", PolicyUnrestricted:
		return domain.Unrestricted(), nil
	case PolicyNoOverdraft:
		return domain.NoOverdraft(), nil
	case PolicyLimit:
		return domain.OverdraftLimit(limit)
	}
	return domain.OverdraftPolicy{}, &domain.Error{Kind: domain.KindInvalidAccount, Detail: "unknown policy " + name}
}

func policyName(p domain.OverdraftPolicy) (string, int64) {
	n, bounded := p.Limit()
	switch {
	case !bounded:
		return PolicyUnrestricted, 0
	case p == domain.NoOverdraft():
		return PolicyNoOverdraft, 0
	}
	return PolicyLimit, n
}

// EncodeEvent turns a domain event into a store record (Stream/Version
// are assigned by the store).
func EncodeEvent(ev domain.Event, at time.Time) (ports.Record, error) {
	var payload any
	switch e := ev.(type) {
	case domain.AccountOpened:
		a := e.Account
		pol, lim := policyName(a.Policy())
		payload = accountDTO{ID: string(a.ID()), Name: a.Name(), Type: a.Type().String(), Currency: a.Currency().String(),
			Policy: pol, Limit: lim, OpenedAt: e.OpenedAt}
	case domain.EntryPosted:
		en := e.Entry
		d := entryDTO{ID: string(en.ID()), PostedAt: en.PostedAt(), Memo: en.Memo(), ReversalOf: string(en.ReversalOf())}
		for _, l := range en.Lines() {
			d.Lines = append(d.Lines, lineDTO{Account: string(l.Account()), Side: l.Side().String(),
				Minor: l.Amount().Minor(), Currency: l.Amount().Currency().String()})
		}
		payload = d
	default:
		return ports.Record{}, fmt.Errorf("app: cannot encode %T", ev)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ports.Record{}, err
	}
	return ports.Record{Type: ev.EventType(), Data: data, RecordedAt: at.UTC()}, nil
}

// DecodeEvent rebuilds a domain event, re-validating it through the
// domain constructors (a record can never bypass invariants).
func DecodeEvent(r ports.Record) (domain.Event, error) {
	switch r.Type {
	case domain.TypeAccountOpened:
		var d accountDTO
		if err := json.Unmarshal(r.Data, &d); err != nil {
			return nil, fmt.Errorf("app: decode %s v%d: %w", r.Stream, r.Version, err)
		}
		a, err := buildAccount(d)
		if err != nil {
			return nil, err
		}
		return domain.AccountOpened{Account: a, OpenedAt: d.OpenedAt}, nil
	case domain.TypeEntryPosted:
		var d entryDTO
		if err := json.Unmarshal(r.Data, &d); err != nil {
			return nil, fmt.Errorf("app: decode %s v%d: %w", r.Stream, r.Version, err)
		}
		e, err := buildEntry(d)
		if err != nil {
			return nil, err
		}
		return domain.EntryPosted{Entry: e}, nil
	}
	return nil, fmt.Errorf("app: unknown event type %q", r.Type)
}

func buildAccount(d accountDTO) (domain.Account, error) {
	typ, err := domain.ParseAccountType(d.Type)
	if err != nil {
		return domain.Account{}, err
	}
	cur, err := domain.ParseCurrency(d.Currency)
	if err != nil {
		return domain.Account{}, err
	}
	pol, err := ParsePolicy(d.Policy, d.Limit)
	if err != nil {
		return domain.Account{}, err
	}
	return domain.NewAccount(domain.AccountID(d.ID), d.Name, typ, cur, pol)
}

func buildLines(in []lineDTO) ([]domain.Line, error) {
	lines := make([]domain.Line, 0, len(in))
	for _, l := range in {
		side, err := domain.ParseSide(l.Side)
		if err != nil {
			return nil, err
		}
		cur, err := domain.ParseCurrency(l.Currency)
		if err != nil {
			return nil, err
		}
		m, err := domain.NewMoney(l.Minor, cur)
		if err != nil {
			return nil, err
		}
		ln, err := domain.NewLine(domain.AccountID(l.Account), side, m)
		if err != nil {
			return nil, err
		}
		lines = append(lines, ln)
	}
	return lines, nil
}

func buildEntry(d entryDTO) (domain.JournalEntry, error) {
	lines, err := buildLines(d.Lines)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	if d.ReversalOf != "" {
		return domain.NewReversalEntry(domain.EntryID(d.ID), d.PostedAt, d.Memo, domain.EntryID(d.ReversalOf), lines...)
	}
	return domain.NewJournalEntry(domain.EntryID(d.ID), d.PostedAt, d.Memo, lines...)
}
