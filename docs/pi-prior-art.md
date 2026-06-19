# Kelp Pi Prior Art

Status: public-source review on 2026-06-19. This is not a market map; it is a
boundary check for adjacent projects mentioned in the Kelp Pi task list.

## Summary

Kelp Pi is closest to a hardened field evidence appliance: scoped AppSec scanning,
offline cited retrieval, policy decisions, hash-chained audit logs, and signed audit
bundles. The adjacent projects below usually optimize for one narrower axis:
offline content, Wi-Fi auditing, portable pentest hardware, or local RAG.

| Project           | Verified public position                                                                                                                                                                                                                 | Where it overlaps                                                                    | Where Kelp Pi differs                                                                                                                                                  |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| xPrep             | No relevant AppSec/Pi/security appliance was verified. Public search results point to unrelated education, CRM, and lab/crystallography products such as Classplus formerly being called XPrep and Bruker XPREP crystallography tooling. | Name-only ambiguity.                                                                 | Treat as unverified prior art unless a specific security product URL is provided. Do not claim differentiation against an unidentified product.                        |
| Internet-in-a-Box | Offline educational hotspot for Raspberry Pi/Linux that serves Wikipedia, Khan Academy, OpenStreetMap, books, and similar content to nearby devices.                                                                                     | Offline local service, Raspberry Pi deployment, useful in disconnected environments. | IIAB is an offline library/learning hotspot, not an AppSec evidence, scanner, policy, or signed audit-bundle appliance.                                                |
| Hak5              | Commercial pentest hardware and software. Hak5 positions products around field kits, on-site implants, Wi-Fi auditing, payload development, and related red-team workflows.                                                              | Portable security hardware and field use.                                            | Kelp Pi is audit-first and policy-gated; it is not a covert implant product line and does not default to exploit execution, rogue AP workflows, or payload delivery.   |
| WiFi Pineapple    | Hak5 Wi-Fi auditing appliance with guided UI, campaigns, recon, and enterprise reporting.                                                                                                                                                | Wireless assessment appliance.                                                       | Kelp Pi is broader AppSec evidence/retrieval/bundle infrastructure. Any Wi-Fi/AP mode in Kelp Pi is for operator access/isolation, not rogue AP assessment by default. |
| Pwnagotchi        | Raspberry Pi Zero W project built on bettercap for capturing crackable WPA key material, including passive sniffing and deauth/association attacks.                                                                                      | Raspberry Pi security device, local/offline operation, Wi-Fi domain.                 | Kelp Pi does not focus on WPA handshakes, deauth, or gamified Wi-Fi attack loops; it focuses on scoped AppSec evidence and reviewer handoff.                           |
| pi-local-rag      | Local hybrid RAG extension for the Pi coding agent: BM25/vector search, local embeddings, file indexing, and zero cloud dependency.                                                                                                      | Offline retrieval, local files, citations/search.                                    | Kelp Pi uses retrieval as one subsystem inside a signed AppSec field appliance with policy gates, scanner/evidence lifecycle, and bundle verification.                 |

## Sources

- [Internet-in-a-Box](https://internet-in-a-box.org/) and [IIAB GitHub](https://github.com/iiab/iiab)
- [Hak5 shop](https://shop.hak5.org/) and [WiFi Pineapple](https://shop.hak5.org/products/wifi-pineapple)
- [Pwnagotchi](https://pwnagotchi.ai/)
- [pi-local-rag GitHub](https://github.com/vahidkowsari/pi-local-rag)
- [EdSurge: Classplus formerly XPrep](https://www.edsurge.com/news/2019-05-15-classplus-raises-1-6-million-to-improve-app-for-tutors)
- [Bruker APEX/XPREP](https://www.bruker.com/en/products-and-solutions/diffractometers-and-x-ray-microscopes/single-crystal-x-ray-diffractometers/sc-xrd-software/apex.html)
