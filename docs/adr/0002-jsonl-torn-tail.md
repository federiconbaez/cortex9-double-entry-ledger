# ADR 0002 — JSONL: truncar solo la cola rota, rechazar corrupción confirmada

- Estado: aceptado
- Ref: CX9-6200

## Contexto

Un crash durante `write` puede dejar la última línea a medias. Una línea completa inválida, en cambio,
indica corrupción de datos ya confirmados (disco, edición manual, bug).

## Decisión

- Cada `Append` = un `write` de líneas terminadas en `\n` + `fsync`; se confirma recién después del `fsync`.
- Al abrir, todo byte posterior al último `\n` se descarta (truncate + fsync) y se informa en `RecoveryReport`:
  por construcción nunca fue confirmado al llamador.
- Una línea completa con JSON inválido, CRC32 distinto o versión fuera de secuencia → `ErrCorrupt`; el store no abre.
- Fallo de `fsync` → el store queda en `ErrStoreFailed`: tras un fsync fallido el estado de la page cache es incierto
  y reintentar puede reportar éxito falso.

## Consecuencias

- (+) Recuperación automática del único caso esperable (torn write) sin perder eventos confirmados.
- (+) La corrupción real se hace visible en vez de ocultarse.
- (−) Ante corrupción el operador debe intervenir manualmente.
- (−) Un solo proceso: no hay lock entre procesos (no-objetivo).
