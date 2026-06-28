# Kelp Pi Prior Art

Last prior-art pass: 2026-06-19. Not rerun in this patch except for model/toolchain checks.

## Position

Kelp Pi is closest to a local AppSec evidence appliance: scoped scanner orchestration, policy-gated tool use, offline retrieval, signed transcripts, and signed audit bundles. Adjacent projects usually optimize for offline content, Wi-Fi auditing, portable pentest hardware, or local RAG.

| Project               | Overlap                                                | Difference                                                                                   |
| --------------------- | ------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| Internet-in-a-Box     | Offline Raspberry Pi service.                          | Offline education library, not AppSec policy/evidence/bundles.                               |
| Hak5 / WiFi Pineapple | Portable security hardware and field workflows.        | Kelp Pi is audit-first and scope-gated; not a covert implant or rogue AP product.            |
| Pwnagotchi            | Raspberry Pi security device, local/offline operation. | Focuses on Wi-Fi handshake capture; Kelp Pi focuses on AppSec evidence and reviewer handoff. |
| pi-local-rag          | Local retrieval and zero cloud dependency.             | Retrieval is one subsystem inside a signed AppSec appliance.                                 |
| xPrep                 | No relevant AppSec/Pi/security appliance verified.     | Treat as unverified unless a specific URL is provided.                                       |

## Sources

- <https://internet-in-a-box.org/>
- <https://github.com/iiab/iiab>
- <https://shop.hak5.org/>
- <https://shop.hak5.org/products/wifi-pineapple>
- <https://pwnagotchi.ai/>
- <https://github.com/vahidkowsari/pi-local-rag>
