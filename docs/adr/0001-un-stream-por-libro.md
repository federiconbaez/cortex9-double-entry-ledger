# ADR 0001 — Un stream de eventos por libro (Ledger) como límite de consistencia

- Estado: aceptado
- Ref: CX9-6200

## Contexto

Un asiento toca ≥ 2 cuentas y debe validarse contra el saldo de todas a la vez (balance + sobregiro).
Si cada cuenta fuera su propio agregado, postear exigiría una saga o transacción multi-stream.

## Decisión

El agregado es el `Ledger`; un stream `ledger-<id>` con control optimista (`expected version`).
El servicio reintenta (máx. 3) ante `ErrVersionConflict` recargando el estado y re-validando el comando.

## Consecuencias

- (+) Invariantes I3/I4/I9 se validan atómicamente en memoria, sin sagas.
- (+) Replay y trial balance triviales de razonar.
- (−) Las escrituras a un mismo libro se serializan; el throughput por libro está acotado.
- (−) Replay O(n) sin snapshots (no-objetivo documentado).
