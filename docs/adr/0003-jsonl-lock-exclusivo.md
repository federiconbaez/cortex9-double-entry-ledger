# ADR 0003 — JSONL: lock exclusivo entre procesos al abrir

- Estado: aceptado
- Ref: CX9-6206

## Contexto

El store JSONL mantiene en memoria el índice de streams y versiones, y valida la versión esperada
contra ese índice. Antes, "un solo proceso" era una regla escrita, pero nadie la hacía cumplir.
Si dos procesos abrían el mismo archivo, cada uno validaba contra su propio índice y los dos podían
escribir la misma versión de un stream. Había además un caso peor: si un segundo `Open` llegaba
mientras el primero estaba a mitad de un `write`, la recuperación del segundo veía esa cola sin
`\n`, la tomaba por rota y truncaba el archivo bajo los pies del escritor.

## Decisión

- `Open` toma un `flock(LOCK_EX|LOCK_NB)` sobre el archivo antes de la recuperación. Si otro
  `Store` lo tiene, en este proceso o en otro, devuelve `jsonlstore.ErrLocked` sin leer ni
  truncar nada.
- El lock pertenece al descriptor: se libera con `Close` o cuando el proceso muere, así que un
  crash no deja un lock huérfano (no hay archivo `.lock` que limpiar).
- Se eligió un escritor único y no un lock por append, porque el índice en memoria no se puede
  invalidar entre procesos sin releer el archivo en cada operación.

## Consecuencias

- Se cumple en Linux, macOS y los BSD (`flock`). En otras plataformas (Windows, Solaris/illumos,
  AIX) `Open` no toma el lock y la regla de un solo proceso vuelve a depender del operador. Los
  tests del lock se saltean ahí.
- `flock` es advisory: un proceso que escriba el archivo sin pasar por `Open` no queda bloqueado.
- En NFS el comportamiento de `flock` depende del servidor; no se soporta.
- Un segundo lector tampoco puede abrir mientras haya un escritor. Para leer en paralelo habría
  que agregar un modo de solo lectura con `LOCK_SH` y recarga del índice; queda fuera de alcance.
