// Command ledger-demo runs a small, deterministic banking scenario on a
// JSONL event store and prints the read model. Run it twice on the same
// file to see the state rebuilt from disk instead of re-posted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/clock"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/ids"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/jsonlstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/app"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
)

func main() {
	path := flag.String("file", "ledger-demo.jsonl", "JSONL event log (created if missing)")
	flag.Parse()
	if err := run(context.Background(), *path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ledger-demo:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path string, out io.Writer) error {
	store, rep, err := jsonlstore.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	fmt.Fprintf(out, "log %s: %d records, %d bytes, torn tail truncated: %d bytes\n",
		path, rep.Records, rep.ValidBytes, rep.TruncatedBytes)

	proj, err := app.Rebuild(ctx, store)
	if err != nil {
		return err
	}
	if rep.Records == 0 {
		clk := clock.NewStepping(time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC), time.Second)
		svc := app.NewService(store, clk, &ids.Sequence{Prefix: "e"}, proj)
		if err := scenario(ctx, svc, out); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "\n%-8s %-10s %-4s %10s %10s %10s\n", "account", "type", "cur", "debits", "credits", "balance")
	for _, r := range proj.Balances("bank") {
		fmt.Fprintf(out, "%-8s %-10s %-4s %10d %10d %10d\n", r.Account, r.Type, r.Currency, r.Debits, r.Credits, r.Balance)
	}
	fmt.Fprintf(out, "trial balance: %v (must be 0 per currency)\n", proj.TrialBalance("bank"))
	return nil
}

func scenario(ctx context.Context, svc *app.Service, out io.Writer) error {
	for _, c := range []app.OpenAccountCmd{
		{Ledger: "bank", Account: "cash", Name: "Caja", Type: "asset", Currency: "ARS"},
		{Ledger: "bank", Account: "wallet", Name: "Billetera", Type: "liability", Currency: "ARS", Policy: app.PolicyNoOverdraft},
		{Ledger: "bank", Account: "fees", Name: "Comisiones", Type: "revenue", Currency: "ARS"},
	} {
		if err := svc.OpenAccount(ctx, c); err != nil {
			return err
		}
	}
	line := func(acct, side string, n int64) app.LineCmd {
		return app.LineCmd{Account: acct, Side: side, Minor: n, Currency: "ARS"}
	}
	dep, err := svc.PostEntry(ctx, app.PostEntryCmd{Ledger: "bank", Memo: "deposito 100,00",
		Lines: []app.LineCmd{line("cash", "debit", 10000), line("wallet", "credit", 10000)}})
	if err != nil {
		return err
	}
	fee, err := svc.PostEntry(ctx, app.PostEntryCmd{Ledger: "bank", Memo: "comision 1,50",
		Lines: []app.LineCmd{line("wallet", "debit", 150), line("fees", "credit", 150)}})
	if err != nil {
		return err
	}
	_, err = svc.PostEntry(ctx, app.PostEntryCmd{Ledger: "bank", Memo: "retiro sin fondos",
		Lines: []app.LineCmd{line("wallet", "debit", 50000), line("cash", "credit", 50000)}})
	fmt.Fprintf(out, "retiro de 500,00 rechazado: %v (insufficient funds: %v)\n", err, errors.Is(err, domain.ErrInsufficientFunds))
	_, err = svc.Reverse(ctx, app.ReverseCmd{Ledger: "bank", EntryID: dep, Memo: "reversa"})
	fmt.Fprintf(out, "reversa del deposito %s rechazada (dejaria la billetera en negativo): %v\n", dep, err)
	rev, err := svc.Reverse(ctx, app.ReverseCmd{Ledger: "bank", EntryID: fee, Memo: "comision devuelta"})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "comision %s revertida por %s\n", fee, rev)
	return nil
}
