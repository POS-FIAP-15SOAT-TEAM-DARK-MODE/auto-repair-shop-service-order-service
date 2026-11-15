# Playbook: Add Migration

## 1. Next Version
```bash
ls migrations/ | sort | tail -4
```

## 2. Create Files
```bash
touch migrations/<NNNNNN>_<description>.up.sql
touch migrations/<NNNNNN>_<description>.down.sql
```
Use the next six-digit version after the last file.

## 3. Write `up.sql`
- `NUMERIC(10,2)` for money; never `FLOAT`.
- `TIMESTAMP WITH TIME ZONE` for timestamps.
- `CHECK` constraints for status and step columns, listing every valid value.
- Foreign keys to the parent table; `ON DELETE CASCADE` only for child rows of an order.
- Indexes on status, foreign key columns and any column used in filters.

## 4. Write `down.sql`
Reverse the `up.sql` in the reverse order: indexes, then foreign keys, columns and tables.

## 5. Apply and Verify
```bash
make migrate-up        # uses POSTGRES_* from .env (see Makefile)
migrate -path migrations -database "$DSN" version
```
Integration tests in CI apply all migrations before running.

## Never
- Edit a migration that has already run in any environment.
- Drop or rename existing columns (add the new column, keep the old one as deprecated).
- Change the schema by hand in any environment.
