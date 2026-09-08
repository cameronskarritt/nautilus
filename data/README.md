# Upload fixtures

This directory contains 21 realistic, non-production upload fixtures: 16 PDFs
and 5 images. The PDFs are official government notices, tax forms, and business
formation templates; the images include two rasterized pages from the included
IRS PDFs and three NASA photographs. Do not use these files as completed legal
or tax documents.

The upload worker currently accepts images and PDFs up to 100 MiB and PDFs with
at most 100 pages. Every file in `pdf/` and `image/` is within those limits.
`boundary/irs-publication-334-57-pages.pdf` is within the page
limit and can exercise longer-document processing.

## Layout

- `pdf/` - expected-success PDF inputs (15 files)
- `image/` - expected-success PNG/JPEG inputs (5 files)
- `boundary/` - longer-document inputs (1 file)

## Sources and provenance

Downloaded 2026-09-07. Keep the source URLs and this readme when copying the
fixtures elsewhere. The source material must not be presented as Nautilus
correspondence, and the files must not be filled with real personal data.

### Federal material

The IRS files are U.S. federal-government material. See [17 U.S.C. section
105](https://www.copyright.gov/title17/92chap1.html#105) and verify any
third-party material called out by the originating document before a use beyond
test fixtures.

| File | Type | Source |
| --- | --- | --- |
| `pdf/irs-cp14-notice.pdf` | Sample balance-due notice | <https://www.irs.gov/pub/notices/cp14_english.pdf> |
| `pdf/irs-cp71c-notice.pdf` | Sample annual balance-due reminder | <https://www.irs.gov/pub/notices/cp71c_english.pdf> |
| `pdf/irs-form-ss4.pdf` | Employer ID application | <https://www.irs.gov/pub/irs-pdf/fss4.pdf> |
| `pdf/irs-form-w9.pdf` | Taxpayer identification request | <https://www.irs.gov/pub/irs-pdf/fw9.pdf> |
| `pdf/irs-form-1099-nec.pdf` | Nonemployee compensation form | <https://www.irs.gov/pub/irs-pdf/f1099nec.pdf> |
| `pdf/irs-form-941.pdf` | Employer quarterly tax return | <https://www.irs.gov/pub/irs-pdf/f941.pdf> |
| `pdf/irs-form-1120.pdf` | U.S. corporation income-tax return | <https://www.irs.gov/pub/irs-pdf/f1120.pdf> |
| `pdf/irs-form-1065.pdf` | Partnership income return | <https://www.irs.gov/pub/irs-pdf/f1065.pdf> |
| `pdf/irs-form-8822b.pdf` | Business address/responsible-party change | <https://www.irs.gov/pub/irs-pdf/f8822b.pdf> |
| `pdf/irs-form-1040.pdf` | Individual income-tax return | <https://www.irs.gov/pub/irs-pdf/f1040.pdf> |
| `pdf/irs-form-2553.pdf` | Small-business corporation election | <https://www.irs.gov/pub/irs-pdf/f2553.pdf> |
| `pdf/irs-publication-583.pdf` | Business recordkeeping publication | <https://www.irs.gov/pub/irs-pdf/p583.pdf> |
| `boundary/irs-publication-334-57-pages.pdf` | 57-page small-business tax guide; within the 100-page limit | <https://www.irs.gov/pub/irs-pdf/p334.pdf> |

`image/irs-cp14-notice-page-1.png` and
`image/irs-form-941-page-1.png` are 200-DPI PNG renderings of page 1 of their
respectively named source PDFs. They are included to test image ingestion of
document layouts rather than only PDFs.

### State business-formation templates

These are unfilled templates downloaded from the named official state source.
They are appropriate for this fixture corpus; recheck the issuer's current
terms before redistributing them outside a development/test context.

| File | Source |
| --- | --- |
| `pdf/delaware-llc-certificate-of-formation.pdf` | <https://corpfiles.delaware.gov/LLC_Forms/LLC%20Formation.pdf> |
| `pdf/delaware-stock-corporation-certificate-of-incorporation.pdf` | <https://corpfiles.delaware.gov/incstk.pdf> |
| `pdf/wyoming-llc-articles-of-organization.pdf` | <https://sos.wyo.gov/Forms/Business/LLC/LLC-ArticlesOrganization.pdf> |

### NASA images

The JPEGs are the medium-size downloads of the named NASA assets. Consult
[NASA's media-usage guidelines](https://www.nasa.gov/nasa-brand-center/images-and-media/)
before use outside this test corpus, especially for agency insignia or material
credited to a non-NASA partner.

| File | NASA asset page | Direct fixture source |
| --- | --- | --- |
| `image/nasa-hubble-deep-field.jpg` | <https://images.nasa.gov/details/PIA12110> | <https://images-assets.nasa.gov/image/PIA12110/PIA12110~medium.jpg> |
| `image/nasa-hubble-telescope.jpg` | <https://images.nasa.gov/details/PIA18165> | <https://images-assets.nasa.gov/image/PIA18165/PIA18165~medium.jpg> |
| `image/nasa-apollo-11.jpg` | <https://images.nasa.gov/details/as11-40-5874> | <https://images-assets.nasa.gov/image/as11-40-5874/as11-40-5874~medium.jpg> |

## Verification

On download, the expected-success PDFs ranged from 2 to 28 pages and 68 KiB to
1.3 MiB. The largest fixture, including the boundary case, is 2.6 MiB; the
whole directory is about 9.2 MiB. `file`, `pdfinfo`, and Poppler rendering were
used to verify the type, page counts, and the two generated PNGs.

To recheck the fixture types and size limits:

```sh
find data/pdf data/image -type f -print0 | xargs -0 file
find data -type f -size +16M -print
for file in data/pdf/*.pdf; do
  pdfinfo "$file" | awk -v file="$file" '/^Pages:/ { print file, $2 }'
done
```
