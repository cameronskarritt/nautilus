# Filled upload fixtures

This is a document-only, happy-path corpus for exercising Nautilus uploads and
OCR. Every document is synthetic and visibly completed: all company names,
people, addresses, account numbers, dates, and amounts are fictional. The
footer on each file makes that explicit, so no fixture can be mistaken for a
usable tax, banking, formation, or insurance record.

## Corpus

- `filled/pdf/` contains 80 one-page, completed PDFs.
- `filled/image/` contains 32 JPEG renderings of completed document pages.

The PDF categories are deliberately balanced: 10 each of invoices, bank
account notices, commercial-insurance renewals, registered-agent notices,
business-tax account notices, vendor letters, entity-formation filings, and
remittance advice. All 112 files are designed to pass the normal upload path;
there are no blank templates, stock photographs, real personal data, or
intentionally-invalid boundary files in this directory.

## Regeneration

The committed files are deterministic. To recreate them with the bundled
document runtime:

```sh
/Users/clsk/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 \
  scripts/generate-upload-fixtures.py
```

The generator writes the 80 PDFs and rasterizes four representative first pages
from each category to 150-DPI JPEGs. It requires ReportLab and Poppler, both
available in the bundled runtime used above.

## Guardrails

Do not replace fictional values with real customer data. Treat the corpus as
test data only, even though its layouts resemble ordinary business mail.
