#!/usr/bin/env python3
"""Generate deterministic, filled document-upload fixtures with fake data."""

from __future__ import annotations

from datetime import date, timedelta
from pathlib import Path
from random import Random
import shutil

from reportlab.lib import colors
from reportlab.lib.pagesizes import LETTER
from reportlab.lib.units import inch
from reportlab.pdfgen.canvas import Canvas


ROOT = Path(__file__).resolve().parents[1]
PDF_DIR = ROOT / "data" / "filled" / "pdf"
IMAGE_DIR = ROOT / "data" / "filled" / "image"
RNG = Random(20260907)

COMPANIES = [
    "Alder Grove Systems LLC", "Beacon Forge Labs Inc.", "Cedar Point Robotics LLC",
    "Dovetail Analytics Inc.", "Emberline Foods LLC", "Fieldstone Freight Inc.",
    "Goldfinch Commerce LLC", "Harbor Peak Health Inc.", "Indigo Ledger LLC",
    "Juniper Current Inc.", "Keystone Orbit LLC", "Lighthouse Works Inc.",
]
ADDRESSES = [
    ("1200 Market Street, Suite 640", "San Francisco, CA 94102"),
    ("410 Lake Avenue, Floor 3", "Chicago, IL 60601"),
    ("88 Summer Street, Suite 1400", "Boston, MA 02110"),
    ("701 Brazos Street, Suite 1600", "Austin, TX 78701"),
    ("200 Peachtree Center Avenue, Suite 900", "Atlanta, GA 30303"),
]
NAMES = ["Avery Chen", "Morgan Patel", "Jordan Rivera", "Riley Thompson", "Casey Nguyen", "Taylor Brooks"]


def money(amount: int) -> str:
    return f"${amount:,.2f}"


def header(canvas: Canvas, title: str, doc_number: str, company: str, addr: tuple[str, str]) -> None:
    canvas.setFillColor(colors.HexColor("#102A43"))
    canvas.rect(0, 10.35 * inch, 8.5 * inch, 0.65 * inch, fill=1, stroke=0)
    canvas.setFillColor(colors.white)
    canvas.setFont("Helvetica-Bold", 17)
    canvas.drawString(0.55 * inch, 10.58 * inch, title)
    canvas.setFillColor(colors.HexColor("#102A43"))
    canvas.setFont("Helvetica-Bold", 11)
    canvas.drawString(0.55 * inch, 10.1 * inch, company)
    canvas.setFont("Helvetica", 9)
    canvas.drawString(0.55 * inch, 9.92 * inch, addr[0])
    canvas.drawString(0.55 * inch, 9.77 * inch, addr[1])
    canvas.setFont("Helvetica-Bold", 9)
    canvas.drawRightString(7.95 * inch, 10.1 * inch, doc_number)


def footer(canvas: Canvas, page: int = 1) -> None:
    canvas.setStrokeColor(colors.HexColor("#CBD5E0"))
    canvas.line(0.55 * inch, 0.55 * inch, 7.95 * inch, 0.55 * inch)
    canvas.setFillColor(colors.HexColor("#718096"))
    canvas.setFont("Helvetica", 7)
    canvas.drawString(0.55 * inch, 0.35 * inch, "SYNTHETIC TEST DOCUMENT - FICTIONAL DATA - NOT VALID")
    canvas.drawRightString(7.95 * inch, 0.35 * inch, f"Page {page} of 1")


def label(canvas: Canvas, text: str, x: float, y: float) -> None:
    canvas.setFillColor(colors.HexColor("#4A5568"))
    canvas.setFont("Helvetica-Bold", 7)
    canvas.drawString(x, y, text.upper())


def value(canvas: Canvas, text: str, x: float, y: float, bold: bool = False) -> None:
    canvas.setFillColor(colors.HexColor("#1A202C"))
    canvas.setFont("Helvetica-Bold" if bold else "Helvetica", 10)
    canvas.drawString(x, y, text)


def line(canvas: Canvas, y: float) -> None:
    canvas.setStrokeColor(colors.HexColor("#CBD5E0"))
    canvas.line(0.55 * inch, y, 7.95 * inch, y)


def invoice(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    client, client_addr = COMPANIES[(i + 3) % len(COMPANIES)], ADDRESSES[(i + 2) % len(ADDRESSES)]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "INVOICE", f"INV-2026-{1001 + i}", company, addr)
    issued = date(2026, 8, 1) + timedelta(days=i)
    label(canvas, "Bill to", 0.55 * inch, 9.25 * inch)
    value(canvas, client, 0.55 * inch, 9.02 * inch, True)
    value(canvas, client_addr[0], 0.55 * inch, 8.84 * inch)
    value(canvas, client_addr[1], 0.55 * inch, 8.68 * inch)
    for label_text, text, y in [("Invoice date", issued.isoformat(), 9.25 * inch), ("Due date", (issued + timedelta(days=30)).isoformat(), 8.98 * inch), ("Terms", "Net 30", 8.71 * inch)]:
        label(canvas, label_text, 6.4 * inch, y)
        canvas.setFont("Helvetica", 9)
        canvas.drawRightString(7.95 * inch, y - 0.16 * inch, text)
    y = 7.95 * inch
    canvas.setFillColor(colors.HexColor("#E2E8F0"))
    canvas.rect(0.55 * inch, y, 7.4 * inch, 0.26 * inch, fill=1, stroke=0)
    canvas.setFillColor(colors.HexColor("#1A202C"))
    canvas.setFont("Helvetica-Bold", 8)
    canvas.drawString(0.68 * inch, y + 0.09 * inch, "DESCRIPTION")
    canvas.drawRightString(7.25 * inch, y + 0.09 * inch, "AMOUNT")
    total = 0
    for row in range(3):
        amount = 950 + i * 63 + row * 285
        total += amount
        y -= 0.5 * inch
        value(canvas, ["Monthly platform services", "Document processing services", "Priority support retainer"][row], 0.68 * inch, y + 0.2 * inch)
        canvas.setFont("Helvetica", 10)
        canvas.drawRightString(7.25 * inch, y + 0.2 * inch, money(amount))
        line(canvas, y)
    label(canvas, "Amount due", 5.75 * inch, 5.82 * inch)
    canvas.setFont("Helvetica-Bold", 16)
    canvas.drawRightString(7.95 * inch, 5.55 * inch, money(total))
    value(canvas, "Please remit payment by ACH using the payment reference above.", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


def bank_notice(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "ACCOUNT ACTIVITY NOTICE", f"BAN-{2026001 + i}", "Fictional National Bank", ("100 Finance Plaza", "New York, NY 10005"))
    value(canvas, company, 0.55 * inch, 9.28 * inch, True)
    value(canvas, addr[0], 0.55 * inch, 9.1 * inch)
    value(canvas, addr[1], 0.55 * inch, 8.94 * inch)
    label(canvas, "Deposit account", 0.55 * inch, 8.35 * inch)
    value(canvas, f"Business Checking ending {2800 + i:04d}", 0.55 * inch, 8.12 * inch, True)
    canvas.setFillColor(colors.HexColor("#EDF2F7"))
    canvas.roundRect(0.55 * inch, 6.35 * inch, 7.4 * inch, 1.15 * inch, 5, fill=1, stroke=0)
    label(canvas, "Activity requiring your attention", 0.75 * inch, 7.15 * inch)
    value(canvas, f"An ACH credit of {money(4200 + i * 119)} posted on 2026-08-{10 + i:02d}.", 0.75 * inch, 6.82 * inch, True)
    value(canvas, "No action is required. Retain this notice with your financial records.", 0.75 * inch, 6.56 * inch)
    label(canvas, "Transaction reference", 0.55 * inch, 5.75 * inch)
    value(canvas, f"ACH-REF-26-{450100 + i}", 0.55 * inch, 5.5 * inch, True)
    label(canvas, "Available balance after posting", 0.55 * inch, 4.9 * inch)
    value(canvas, money(18400 + i * 931), 0.55 * inch, 4.64 * inch, True)
    value(canvas, "Questions? Call 800-555-0147 and reference the notice number above.", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


def insurance_notice(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "COMMERCIAL POLICY RENEWAL", f"POL-CL-{67001 + i}", "Harbor Mutual Insurance", ("55 Beacon Street", "Boston, MA 02108"))
    value(canvas, company, 0.55 * inch, 9.28 * inch, True)
    value(canvas, addr[0], 0.55 * inch, 9.1 * inch)
    value(canvas, addr[1], 0.55 * inch, 8.94 * inch)
    label(canvas, "Policy period", 0.55 * inch, 8.35 * inch)
    value(canvas, "2026-10-01 through 2027-10-01", 0.55 * inch, 8.1 * inch, True)
    label(canvas, "Coverage", 0.55 * inch, 7.62 * inch)
    value(canvas, "General liability and business property", 0.55 * inch, 7.37 * inch)
    canvas.setFillColor(colors.HexColor("#FFF5F5"))
    canvas.roundRect(0.55 * inch, 5.8 * inch, 7.4 * inch, 1.1 * inch, 5, fill=1, stroke=0)
    label(canvas, "Premium due", 0.75 * inch, 6.58 * inch)
    value(canvas, money(2180 + i * 175), 0.75 * inch, 6.25 * inch, True)
    due_date = date(2026, 9, 1) + timedelta(days=i)
    value(canvas, f"Payment is due by {due_date.isoformat()} to continue coverage without interruption.", 2.5 * inch, 6.25 * inch)
    label(canvas, "Broker of record", 0.55 * inch, 4.95 * inch)
    value(canvas, f"{NAMES[i % len(NAMES)]}, Northwind Risk Partners", 0.55 * inch, 4.7 * inch)
    value(canvas, "Review the policy summary and notify your broker of any material business changes.", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


def registered_agent_notice(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    state = ["Delaware", "Wyoming", "Nevada", "New Mexico", "Colorado"][i % 5]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "REGISTERED AGENT SERVICE NOTICE", f"RA-{2026001 + i}", "Civic Registered Agent Services", ("401 Federal Street, Suite 4", "Dover, DE 19901"))
    value(canvas, company, 0.55 * inch, 9.28 * inch, True)
    value(canvas, addr[0], 0.55 * inch, 9.1 * inch)
    value(canvas, addr[1], 0.55 * inch, 8.94 * inch)
    label(canvas, "Jurisdiction", 0.55 * inch, 8.3 * inch)
    value(canvas, state, 0.55 * inch, 8.05 * inch, True)
    label(canvas, "Matter", 0.55 * inch, 7.55 * inch)
    value(canvas, "Annual report and registered-agent service renewal", 0.55 * inch, 7.3 * inch, True)
    canvas.setFillColor(colors.HexColor("#EBF8FF"))
    canvas.roundRect(0.55 * inch, 5.6 * inch, 7.4 * inch, 1.18 * inch, 5, fill=1, stroke=0)
    label(canvas, "Action requested", 0.75 * inch, 6.48 * inch)
    due_date = date(2026, 9, 1) + timedelta(days=i)
    value(canvas, f"Confirm your registered-office details by {due_date.isoformat()}.", 0.75 * inch, 6.14 * inch, True)
    value(canvas, "No change is required if the entity name, address, and authorized contact remain current.", 0.75 * inch, 5.88 * inch)
    label(canvas, "Entity file number", 0.55 * inch, 4.8 * inch)
    value(canvas, f"{7300000 + i}", 0.55 * inch, 4.55 * inch, True)
    value(canvas, "This notice is a service reminder and is not a filing made with a state agency.", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


def tax_notice(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "BUSINESS TAX ACCOUNT NOTICE", f"TX-{2026001 + i}", "Department of Revenue", ("Revenue Processing Center", "Capital City, ST 00000"))
    value(canvas, company, 0.55 * inch, 9.28 * inch, True)
    value(canvas, addr[0], 0.55 * inch, 9.1 * inch)
    value(canvas, addr[1], 0.55 * inch, 8.94 * inch)
    label(canvas, "Account number", 0.55 * inch, 8.3 * inch)
    value(canvas, f"TX-{19 + i:02d}-{480000 + i}", 0.55 * inch, 8.05 * inch, True)
    label(canvas, "Tax period", 3.2 * inch, 8.3 * inch)
    value(canvas, "Quarter ended 2026-06-30", 3.2 * inch, 8.05 * inch, True)
    canvas.setFillColor(colors.HexColor("#FFF5F5"))
    canvas.roundRect(0.55 * inch, 5.75 * inch, 7.4 * inch, 1.35 * inch, 5, fill=1, stroke=0)
    label(canvas, "Balance due", 0.75 * inch, 6.72 * inch)
    value(canvas, money(360 + i * 41), 0.75 * inch, 6.35 * inch, True)
    due_date = date(2026, 9, 1) + timedelta(days=i)
    value(canvas, f"Please pay or respond by {due_date.isoformat()} to avoid additional charges.", 2.5 * inch, 6.35 * inch)
    value(canvas, "Our records show a return was received, but the remittance was short by the amount shown.", 0.75 * inch, 5.98 * inch)
    label(canvas, "Payment reference", 0.55 * inch, 4.82 * inch)
    value(canvas, f"PAY-{2026}{i:04d}-R", 0.55 * inch, 4.57 * inch, True)
    value(canvas, "If you believe this notice is incorrect, submit a written explanation with supporting records.", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


def vendor_letter(path: Path, i: int) -> None:
    sender, sender_addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    recipient, recipient_addr = COMPANIES[(i + 4) % len(COMPANIES)], ADDRESSES[(i + 1) % len(ADDRESSES)]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "BUSINESS CORRESPONDENCE", f"LTR-{2026001 + i}", sender, sender_addr)
    value(canvas, "August 2026", 0.55 * inch, 9.25 * inch)
    value(canvas, recipient, 0.55 * inch, 8.72 * inch, True)
    value(canvas, recipient_addr[0], 0.55 * inch, 8.54 * inch)
    value(canvas, recipient_addr[1], 0.55 * inch, 8.38 * inch)
    value(canvas, f"Attn: {NAMES[i % len(NAMES)]}", 0.55 * inch, 8.05 * inch)
    label(canvas, "Re", 0.55 * inch, 7.45 * inch)
    value(canvas, "Confirmation of service and remittance details", 0.55 * inch, 7.2 * inch, True)
    paragraphs = [
        "This letter confirms that the scheduled business services for the current period were completed as agreed.",
        f"The enclosed remittance advice references payment {money(1500 + i * 93)} and should be retained with your accounting records.",
        "Please notify us promptly if your billing contact or remittance address has changed.",
        "Sincerely,",
        NAMES[(i + 2) % len(NAMES)] + "\nOperations Manager",
    ]
    y = 6.65 * inch
    for text in paragraphs:
        for part in text.split("\n"):
            value(canvas, part, 0.55 * inch, y)
            y -= 0.26 * inch
        y -= 0.16 * inch
    footer(canvas)
    canvas.save()


def formation_filing(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    state = ["DELAWARE", "WYOMING", "COLORADO", "NEW MEXICO", "NEVADA"][i % 5]
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, f"{state} CERTIFICATE OF FORMATION", f"FIL-{2026001 + i}", "Secretary of State Filing Office", ("Public Records Division", "Capital City, ST 00000"))
    label(canvas, "Filed entity name", 0.75 * inch, 9.25 * inch)
    value(canvas, company, 0.75 * inch, 8.97 * inch, True)
    line(canvas, 8.82 * inch)
    label(canvas, "Registered office", 0.75 * inch, 8.4 * inch)
    value(canvas, addr[0], 0.75 * inch, 8.13 * inch)
    value(canvas, addr[1], 0.75 * inch, 7.93 * inch)
    label(canvas, "Registered agent", 0.75 * inch, 7.45 * inch)
    value(canvas, "Civic Registered Agent Services", 0.75 * inch, 7.18 * inch, True)
    label(canvas, "Purpose", 0.75 * inch, 6.7 * inch)
    value(canvas, "To engage in any lawful act or activity for which entities may be organized.", 0.75 * inch, 6.43 * inch)
    label(canvas, "Effective date", 0.75 * inch, 5.92 * inch)
    value(canvas, f"2026-08-{(i % 20) + 1:02d}", 0.75 * inch, 5.65 * inch, True)
    canvas.setStrokeColor(colors.HexColor("#4A5568"))
    canvas.line(0.75 * inch, 3.2 * inch, 3.5 * inch, 3.2 * inch)
    value(canvas, NAMES[i % len(NAMES)], 0.75 * inch, 2.95 * inch, True)
    value(canvas, "Authorized person", 0.75 * inch, 2.77 * inch)
    footer(canvas)
    canvas.save()


def remittance(path: Path, i: int) -> None:
    company, addr = COMPANIES[i % len(COMPANIES)], ADDRESSES[i % len(ADDRESSES)]
    vendor, vendor_addr = COMPANIES[(i + 5) % len(COMPANIES)], ADDRESSES[(i + 3) % len(ADDRESSES)]
    amount = 2300 + i * 82
    canvas = Canvas(str(path), pagesize=LETTER)
    header(canvas, "REMITTANCE ADVICE", f"REM-{2026001 + i}", company, addr)
    label(canvas, "Payee", 0.55 * inch, 9.25 * inch)
    value(canvas, vendor, 0.55 * inch, 9.0 * inch, True)
    value(canvas, vendor_addr[0], 0.55 * inch, 8.82 * inch)
    value(canvas, vendor_addr[1], 0.55 * inch, 8.66 * inch)
    label(canvas, "Payment date", 5.75 * inch, 9.25 * inch)
    value(canvas, f"2026-08-{(i % 25) + 1:02d}", 5.75 * inch, 9.0 * inch, True)
    canvas.setFillColor(colors.HexColor("#F7FAFC"))
    canvas.rect(0.55 * inch, 6.65 * inch, 7.4 * inch, 0.31 * inch, fill=1, stroke=0)
    label(canvas, "Invoice", 0.7 * inch, 6.77 * inch)
    label(canvas, "Description", 2.5 * inch, 6.77 * inch)
    label(canvas, "Amount paid", 6.7 * inch, 6.77 * inch)
    value(canvas, f"INV-2026-{2100 + i}", 0.7 * inch, 6.35 * inch)
    value(canvas, "Services rendered", 2.5 * inch, 6.35 * inch)
    canvas.setFont("Helvetica-Bold", 10)
    canvas.drawRightString(7.75 * inch, 6.35 * inch, money(amount))
    line(canvas, 6.02 * inch)
    label(canvas, "Total payment", 5.4 * inch, 5.45 * inch)
    canvas.setFont("Helvetica-Bold", 15)
    canvas.drawRightString(7.95 * inch, 5.16 * inch, money(amount))
    value(canvas, f"Payment reference: ACH-{20260000 + i}", 0.55 * inch, 1.15 * inch)
    footer(canvas)
    canvas.save()


BUILDERS = [invoice, bank_notice, insurance_notice, registered_agent_notice, tax_notice, vendor_letter, formation_filing, remittance]
PREFIXES = ["invoice", "bank-notice", "insurance-renewal", "registered-agent", "tax-notice", "vendor-letter", "formation-filing", "remittance"]


def main() -> None:
    if PDF_DIR.parent.exists():
        shutil.rmtree(PDF_DIR.parent)
    PDF_DIR.mkdir(parents=True)
    IMAGE_DIR.mkdir(parents=True)
    for category, (prefix, builder) in enumerate(zip(PREFIXES, BUILDERS)):
        for index in range(10):
            builder(PDF_DIR / f"{prefix}-{index + 1:02d}.pdf", category * 10 + index)

    # Image-only upload fixtures: first pages rendered at 150 DPI from each document class.
    selected = [PDF_DIR / f"{prefix}-{index:02d}.pdf" for prefix in PREFIXES for index in range(1, 5)]
    for pdf in selected:
        target = IMAGE_DIR / pdf.stem
        import subprocess
        subprocess.run(
            ["pdftoppm", "-f", "1", "-l", "1", "-r", "150", "-jpeg", "-singlefile", str(pdf), str(target)],
            check=True,
        )


if __name__ == "__main__":
    main()
