# cortex9-double-entry-ledger

Ledger de **partida doble inmutable** en Go con *event sourcing* + CQRS y arquitectura hexagonal.
Sin dependencias externas (solo stdlib). Ref: CX9-6200. Licencia Apache-2.0.

El contrato (alcance, no-objetivos, invariantes, errores, puertos, criterios de aceptación) está en
[CONTRACT.md](CONTRACT.md) y se escribió antes del código. Decisiones: [ADR 0001](docs/adr/0001-un-stream-por-libro.md)
(un stream por libro) y [ADR 0002](docs/adr/0002-jsonl-torn-tail.md) (recuperación del JSONL).

## Qué hace

- **Dominio puro** (`internal/domain`): `Money` en `int64` de unidades menores + moneda ISO (nunca `float`),
  aritmética con chequeo de desborde y de moneda, `Account` con política de sobregiro, `JournalEntry`
  inmutable (≥ 2 líneas, Σdébitos = Σcréditos **por moneda**), agregado `Ledger` que solo cambia aplicando
  eventos, reversiones como asientos espejo (una por asiento), errores tipados (`errors.Is`).
- **Specification**: `SufficientBalance = WithinOverdraftPolicy OR NonWorsening` (un cliente en rojo siempre puede pagar).
- **Puertos**: `EventStore` (append con versión esperada / concurrencia optimista), `Clock`, `IDGenerator`.
- **Aplicación** (`internal/app`): `Service` (solo orquesta), `LedgerRepository`, `UnitOfWork` (un agregado
  por transacción), reintento acotado (3) ante conflicto de versión re-validando el comando, codec de eventos
  versionado fuera del dominio, y `BalanceProjection` (read model CQRS) idempotente por versión y con *catch-up*
  desde el store si otro escritor intercaló eventos.
- **Adaptadores**: store en memoria; store **JSONL** append-only (un `write` + `fsync` por append, CRC32 por línea,
  recuperación de cola rota, store "roto" tras un `fsync` fallido); reloj de sistema y reloj escalonado; IDs aleatorios y secuenciales.
- `cmd/ledger-demo`: escenario determinista sobre un archivo JSONL.

## Qué NO hace (honesto)

- No es un core bancario productivo: sin API HTTP, auth, base de datos, multi-tenant ni cierre de períodos.
- Sin conversión de monedas ni exponentes ISO 4217 (la UI formatea `10000` → `100,00`).
- Sin snapshots: cargar un libro es O(eventos del libro). Las escrituras a un mismo libro se serializan (ADR 0001).
- El store JSONL es de **un proceso**: no hay lock de archivo entre procesos.
- No hay transacciones entre libros (el `UnitOfWork` rechaza un segundo agregado).
- `EntryID` funciona como clave de idempotencia: un reintento con el mismo id se rechaza como `ErrDuplicateEntry`
  (no devuelve "ok" idempotente).
- Si la proyección falla después de confirmar, el comando devuelve `ErrProjectionStale` aunque los eventos **sí** quedaron escritos; se repara con `app.Rebuild`.
- Go no mide cobertura de ramas: el número de abajo es de **sentencias**.

## Invariantes y cómo se verifican

| Invariante | Verificación |
|---|---|
| Trial balance = 0 por moneda | property test (`testing/quick`, 300 semillas × 60 comandos, mezcla aceptados/rechazados) |
| Comando rechazado no muta nada | mismo property test compara un snapshot completo antes/después |
| Replay determinista | property test: replay ×2 == estado vivo |
| Saldos = fold de eventos | property test contra un fold ingenuo independiente |
| Proyección == fold | property test vía `Service` sobre memoria (100 semillas) y JSONL con reapertura (20 semillas) |
| Concurrencia optimista | suite de contrato: 16 goroutines con la misma versión → exactamente 1 gana |
| Regla de dependencias | `TestDomainDependencyRule` (whitelist de imports) + `depguard` en golangci-lint |

La suite de contrato `internal/ports/eventstoretest` corre idéntica contra el store en memoria **y** el JSONL.

## Números medidos (local, Go 1.27.3, linux/amd64, 2026-11-25)

- `go test -race -count=1 ./...`: verde. **58 tests top-level + 46 subtests = 104**, 0 fallos.
- Cobertura de sentencias del dominio: **99,4 %** (`go test -coverprofile ./internal/domain/`).
  App 97,4 %, JSONL 93,2 %, memstore 100 %.
- Fuzz `FuzzScan` (decoder JSONL): 60 s locales, ~9,0 M ejecuciones, 0 fallos. En CI corre 30 s.
- gofmt, go vet, staticcheck, golangci-lint (depguard, errorlint, gosec, revive): 0 issues. gitleaks: sin hallazgos.

No hay benchmarks de throughput: no se publican números que no se midieron.

## Bug encontrado durante el desarrollo (con test de regresión)

La primera versión alimentaba la proyección solo con los eventos del propio comando. Con otro escritor
intercalado (otra instancia del servicio) la proyección recibía `v7` después de `v4` y fallaba con
`ErrProjectionGap`. Ahora el `UnitOfWork` hace *catch-up* leyendo el stream (es seguro porque `Apply` es
idempotente). Test: `TestOptimisticConcurrencyRetry` en `internal/app/service_test.go`.

## Cómo reproducir

```bash
gofmt -l .                       # sin salida
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
golangci-lint run ./...
go test -race -count=1 ./...
go test -count=1 -coverprofile=domain.out ./internal/domain/ && go tool cover -func=domain.out | tail -1
go test -run='^$' -fuzz=FuzzScan -fuzztime=60s ./internal/adapters/jsonlstore/
go run ./cmd/ledger-demo -file /tmp/demo.jsonl   # correrlo 2 veces: la 2ª reconstruye desde disco
```

Para ver la recuperación: cortá a mano la última línea de `/tmp/demo.jsonl` (sin `\n`) y volvé a correr la demo;
informa `torn tail truncated: N bytes` y el libro queda con los eventos confirmados. Si en cambio rompés una línea
completa del medio, el store se niega a abrir con `ErrCorrupt` y el número de línea.

## Estructura

```
internal/domain                 modelo puro (sin I/O)
internal/ports                  EventStore, Clock, IDGenerator
internal/ports/eventstoretest   suite de contrato para cualquier EventStore
internal/app                    Service, Repository, UnitOfWork, codec, BalanceProjection
internal/adapters/{memstore,jsonlstore,clock,ids}
cmd/ledger-demo
```

## Cómo contarlo en una entrevista

1. **El problema**: un ledger donde el dinero no se crea ni se destruye. Lo modelé como partida doble: cada asiento
   balancea por moneda, y la suma de todos los saldos es cero siempre. Eso no es un test aislado: es un property test
   sobre miles de secuencias aleatorias.
2. **Por qué event sourcing**: los saldos no se guardan, se derivan. La historia es inmutable; corregir es postear una
   reversión, como en contabilidad real. Auditoría y replay salen gratis.
3. **Límite de consistencia**: el agregado es el libro, no la cuenta, para validar todas las cuentas de un asiento a la vez
   sin sagas. El costo (escrituras serializadas por libro) está escrito en un ADR.
4. **Concurrencia**: versión esperada en el append; ante conflicto recargo y re-valido el comando, nunca reaplico eventos viejos a ciegas.
5. **Durabilidad**: un write + fsync por append; una cola sin `\n` nunca se confirmó, así que se trunca; una línea completa
   corrupta es historia confirmada dañada y no se repara sola. Tras un fsync fallido el store deja de aceptar escrituras.
6. **Hexagonal de verdad**: el dominio no importa nada fuera de una whitelist de stdlib y lo hacen cumplir un test y depguard.
   Los adaptadores pasan la misma suite de contrato.
7. **Qué cambiaría en producción**: snapshots, store transaccional (Postgres) con outbox para la proyección,
   lock entre procesos, y formato monetario con exponente ISO.

## Licencia

Apache-2.0. Ver [LICENSE](LICENSE).
