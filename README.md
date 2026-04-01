# AFNOR Schematron Validator

Web application for validating XML invoices against French AFNOR Schematron rules (CTC-FR).  
Built with Go and Saxon-HE, it runs XSLT/Schematron validations and reports errors and warnings via a browser interface.

---

## Supported validations

| ID | Description |
|----|-------------|
| `EXTENDED-CTC-FR-UBL` | EXTENDED-CTC-FR-UBL V1.3 — two-pass validation (EXTENDED + BR-FR-Flux2) |
| `EXTENDED-CTC-FR-CDAR` | EXTENDED-CTC-FR-CDAR V1.3.0 — single-pass validation |

---

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| [Go](https://go.dev/dl/) | ≥ 1.21 | Used to build the application |
| [Java](https://adoptium.net/) | ≥ 11 (JRE) | Required at runtime to execute Saxon |

### JAR dependencies (included in repository)

The following JARs are versioned in this repository and require no manual download:

| File | Purpose |
|------|---------|
| `saxon-he-12.9.jar` | Saxon-HE XSLT/XQuery processor |
| `xmlresolver-6.0.9.jar` | XML catalog resolver (Saxon dependency) |

> If you need to update them, download from:
> - **Saxon-HE**: [saxonica.com/download](https://www.saxonica.com/download/download_page.xml) → `saxon-he-*.jar`
> - **XML Resolver**: [GitHub releases](https://github.com/xmlresolver/xmlresolver/releases) → `xmlresolver-*.jar`
>
> Replace the JAR files in the project root and commit.

---

## Installation

```bash
# 1. Clone the repository (JARs are included)
git clone <repo-url>
cd afnor-schematron

# 2. Build
go build -o validator.exe .
```

---

## Usage

```bash
# Start the server (default port: 3000)
./validator.exe
```

Then open [http://localhost:3000](http://localhost:3000) in your browser.

1. Select an XML file to validate
2. Choose the validation type
3. Click **Valider** — results appear below

---

## Configuration

All configuration is done via environment variables.

### Port

```bash
# Windows
set PORT=8080
./validator.exe

# Linux / macOS
PORT=8080 ./validator.exe
```

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | TCP port the HTTP server listens on |

### Saxon JAR path

By default the application searches for `saxon*.jar` in the current directory.  
To use a JAR located elsewhere:

```bash
# Windows
set SAXON_JAR=C:\libs\saxon-he-12.9.jar
./validator.exe

# Linux / macOS
SAXON_JAR=/opt/libs/saxon-he-12.9.jar ./validator.exe
```

| Variable | Default | Description |
|----------|---------|-------------|
| `SAXON_JAR` | *(auto-detect `saxon*.jar` in current dir)* | Explicit path to the Saxon JAR |

---

## Project structure

```
afnor-schematron/
├── main.go                         # HTTP server, Saxon runner, SVRL parser, HTML template
├── go.mod                          # Go module definition
├── saxon-he-12.9.jar               # Saxon-HE processor (versioned)
├── xmlresolver-6.0.9.jar           # XML Resolver (versioned)
├── data/
│   ├── ubl/
│   │   ├── EXTENDED-CTC-FR-UBL-V1.3.0.xsl
│   │   ├── EXTENDED-CTC-FR-UBL-V1.3.0.sch
│   │   ├── BR-FR-Flux2-Schematron-UBL_V1.3.0.xsl
│   │   └── BR-FR-Flux2-Schematron-UBL_V1.3.0.sch
│   └── cdar/
│       └── BR-FR-CDV-Schematron-CDAR_V1.3.0.xsl
└── .gitignore
```

---

## How it works

1. The user uploads an XML file via the browser form.
2. The server writes the file to a temporary location.
3. Saxon-HE is invoked via `java -cp` with the compiled XSLT (pre-compiled from Schematron).
4. The SVRL output is parsed — `failed-assert` elements become errors or warnings based on their `flag` attribute.
5. Results are rendered in the browser.

---

## License

Application source code is licensed under the [MIT License](LICENSE).

Schematron rules (`data/`) are published by [AFNOR](https://www.afnor.org/) under their respective terms.
