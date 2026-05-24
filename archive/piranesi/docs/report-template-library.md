# Local Report Template Library

Piranesi can include operator-owned local report text without using AI generation. A template
library is a JSON file stored wherever the operator keeps local engagement materials.

```json
{
  "schema_version": "piranesi.template-library.v1",
  "templates": [
    {
      "id": "methodology:web",
      "kind": "methodology",
      "title": "Web Assessment Methodology",
      "version": "v1",
      "body": "Reviewed imported web assessment evidence and mapped findings to preserved source artifacts."
    },
    {
      "id": "remediation:headers",
      "kind": "remediation",
      "title": "Security Header Hardening",
      "version": "v1",
      "body": "Set security headers at the edge or application layer and retest affected routes."
    }
  ]
}
```

Include selected templates when rendering a pentest report:

```bash
piranesi report \
  --workspace ./workspace \
  --format md \
  --template-library ./templates/report-text.json \
  --template methodology:web \
  --template remediation:headers
```

`methodology` templates are appended to the methodology list. `remediation` and `section`
templates are rendered in the report's template text section. The report appendices record each
selected template's id, kind, title, and version so generated artifacts remain traceable to local
source text.
