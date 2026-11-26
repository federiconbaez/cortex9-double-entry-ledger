# CONTRACT — cortex9-double-entry-ledger (CX9-6200)

Contrato escrito **antes** de implementar. Si el código y este documento discrepan, el bug está en el código.

## 1. Alcance

Ledger de partida doble **inmutable** con *event sourcing* y CQRS, en Go, sin dependencias externas.

- Dominio puro (`internal/domain`): `Currency`, `Money`, `Account`, `Line`, `JournalEntry`, agregado `Ledger`, eventos, especificaciones, errores tipados.
- Puertos (`internal/ports`): `EventStore`, `Clock`, `IDGenerator`.
- Aplicación (`internal/app`): servicio de comandos, `Repository`, `UnitOfWork`, codec de eventos, proyección de saldos (read model).
- Adaptadores (`internal/adapters`): event store en memoria, event store en archivo JSONL (append-only, fsync), reloj de sistema / reloj escalonado, generadores de IDs.
- `cmd/ledger-demo`: escenario reproducible sobre un archivo JSONL.

## 2. No-objetivos

- No es un core bancario productivo: sin autenticación, multi-tenant, HTTP/gRPC ni base de datos.
- Sin conversión de monedas ni tipos de cambio: cada moneda balancea por separado.
- Sin formato de exponente ISO 4217 (`Money` guarda unidades menores; mostrar "123,45" es responsabilidad de la UI).
- Sin snapshots, compaction ni retención: el replay es O(eventos del stream).
- El store JSONL tiene **un solo escritor** y un solo archivo. Desde CX9-6206 lo hace cumplir un
  `flock` exclusivo en Linux, macOS y los BSD (ADR 0003). En otras plataformas no hay lock.
  No hay lectores concurrentes desde otro proceso ni soporte NFS.
- Un `UnitOfWork` confirma **un** agregado: no hay transacciones multi-stream.
- Sin cierre contable de períodos ni reportes regulatorios.

## 3. Modelo

- Un `Ledger` (libro) es el agregado y el límite de consistencia: un stream de eventos por libro (`ledger-<id>`).
- Eventos: `AccountOpened`, `EntryPosted`. Una reversión es un `EntryPosted` cuyo asiento tiene `ReversalOf`.
- Saldo con signo *debit-positive* por cuenta; el saldo "normal" se expresa según el lado natural del tipo
  (Asset/Expense: débito; Liability/Equity/Revenue: crédito).

## 4. Invariantes

| ID | Invariante |
|----|-----------|
| I1 | `Money` es entero `int64` de unidades menores + moneda ISO (3 letras A-Z). Nunca `float`. |
| I2 | Aritmética entre monedas distintas → `ErrCurrencyMismatch`; desbordes de `int64` → `ErrOverflow`. Nunca se envuelve en silencio. |
| I3 | Un asiento tiene ≥ 2 líneas, montos > 0, y por **cada** moneda Σdébitos = Σcréditos. |
| I4 | Cada línea referencia una cuenta existente del libro y usa la moneda de esa cuenta. |
| I5 | Los asientos son inmutables: no hay update ni delete; la corrección es un asiento de reversión, a lo sumo una por asiento, y una reversión no se revierte. |
| I6 | Los saldos se derivan **solo** de eventos. Trial balance (Σ saldos con signo por moneda) = 0 siempre. |
| I7 | Replay determinista: reproducir los mismos eventos produce el mismo estado. |
| I8 | La proyección de saldos (read model) = fold de los eventos del store. |
| I9 | Política de sobregiro por cuenta (`Unrestricted`, `NoOverdraft`, `OverdraftLimit(n)`) evaluada con una `Specification` al postear; un movimiento que no empeora el saldo normal siempre se permite. |
| I10 | Un comando rechazado no deja eventos pendientes ni muta el estado. |
| I11 | `Append` con versión esperada distinta a la actual → `ErrVersionConflict`, sin escribir nada. |

## 5. Errores (tipados, comparables con `errors.Is`)

Dominio (`domain.Error{Kind, Detail}`): `ErrInvalidCurrency`, `ErrInvalidID`, `ErrInvalidAmount`, `ErrCurrencyMismatch`,
`ErrOverflow`, `ErrTooFewLines`, `ErrUnbalanced`, `ErrInvalidAccount`, `ErrDuplicateAccount`, `ErrUnknownAccount`,
`ErrDuplicateEntry`, `ErrUnknownEntry`, `ErrAlreadyReversed`, `ErrInsufficientFunds`, `ErrCorruptHistory`.

Puertos/adaptadores: `ports.ErrVersionConflict` (vía `*ports.ConflictError`), `jsonlstore.ErrCorrupt`, `jsonlstore.ErrStoreFailed`,
`jsonlstore.ErrLocked` (otro `Store` tiene el archivo), `jsonlstore.ErrClosed`.

## 6. Puertos

```go
type EventStore interface {
    Append(ctx context.Context, stream string, expected uint64, recs []Record) (uint64, error)
    Load(ctx context.Context, stream string) ([]Record, error)
    ReadAll(ctx context.Context) ([]Record, error) // orden global de append
}
type Clock interface{ Now() time.Time }
type IDGenerator interface{ NewID() string }
```

El store asigna versiones `expected+1 … expected+n`. `Data` es opaco para el store.

## 7. JSONL: durabilidad y recuperación

- Una línea por evento: `{"crc":<crc32 IEEE de rec>,"rec":{...}}\n`. Un `Append` escribe todas sus líneas con **un** `write` y luego `fsync`; solo después confirma.
- Al abrir: una cola final **sin `\n`** es una escritura rota nunca confirmada → se trunca el archivo hasta el último `\n` válido, se hace `fsync` y se informa en `RecoveryReport`.
- Una línea **completa** inválida (JSON roto, CRC distinto, versión fuera de secuencia) → `ErrCorrupt` con número de línea: el store **no abre**. No se "repara" historia confirmada.
- Si `write` falla se intenta truncar al tamaño previo; si `fsync` falla el store queda en `ErrStoreFailed` (no se reintenta un fsync fallido).
- Antes de recuperar, `Open` toma un lock exclusivo no bloqueante. Si no lo consigue devuelve `ErrLocked` sin leer ni
  truncar. El lock se libera con `Close` o al morir el proceso.

## 8. Criterios de aceptación

1. CONTRACT.md + ADRs antes del código.
2. `internal/domain` sin I/O ni imports fuera de una whitelist de stdlib (test de imports + depguard).
3. Tests de dominio con fakes escritos a mano (sin librerías de mocks).
4. Property tests (`testing/quick`): trial balance = 0, replay determinista, proyección = fold, comandos rechazados sin efecto.
5. Suite de contrato del `EventStore` corriendo contra memoria **y** JSONL.
6. Fuzz nativo del decoder JSONL y del codec de eventos (corridas cortas en CI).
7. Gates en CI: gofmt, go vet, staticcheck, golangci-lint, `go test -race`, cobertura de dominio reportada, gitleaks. Verde en `main`.
8. README en español honesto, con solo números medidos.
