# Kelp Pi Hardware

## Reference Unit

Reference field unit: Raspberry Pi 5 8GB with the official active cooler, official 27W
USB-C PSU, and official 256GB Raspberry Pi SSD Kit. This is the P0 baseline for build,
test, and docs unless a task explicitly names the 16GB profile.

## Parts List

| Part                                          | Exact SKU           | Qty | Vendor link                                                                                                                   |
| --------------------------------------------- | ------------------- | --: | ----------------------------------------------------------------------------------------------------------------------------- |
| Raspberry Pi 5 8GB board                      | Raspberry Pi SC1112 |   1 | [Raspberry Pi 5 8GB, PiShop.us](https://www.pishop.us/product/raspberry-pi-5-8gb/)                                            |
| Raspberry Pi Active Cooler                    | Raspberry Pi SC1148 |   1 | [Raspberry Pi Active Cooler, Newark](https://www.newark.com/raspberry-pi/sc1148/cooling-fan-raspberry-pi-5-model/dp/82AK3950) |
| Raspberry Pi 27W USB-C Power Supply, US black | Raspberry Pi SC1158 |   1 | [Raspberry Pi 27W USB-C PSU, Micro Center](https://www.microcenter.com/product/671927/raspberry-pi-27w-usb-c-psu-black)       |
| Raspberry Pi SSD Kit 256GB                    | Raspberry Pi SC1675 |   1 | [Raspberry Pi SSD Kit 256GB, PiShop.us](https://www.pishop.us/product/raspberry-pi-m-2-hat-ssd-kit-for-raspberry-pi-5-256gb/) |

## Storage Decision

Use NVMe via the official Raspberry Pi SSD Kit as the reference storage path.
microSD A2 64GB remains acceptable only as a bootstrap or recovery medium.

Rationale: the official 256GB Raspberry Pi SSD Kit is specified at 40k 4KB random
read IOPS and 70k 4KB random write IOPS. Official Raspberry Pi A2 microSD cards on
Pi 5 are specified at 5k 4KB random read IOPS and 2k 4KB random write IOPS. Kelp Pi
does steady-state SQLite FTS5 ingest, scanner JSONL/evidence append, audit-log hash
chaining, and bundle export, so the reference unit should prefer the 8x read and
35x write random-I/O headroom of NVMe over microSD.

## Source Notes

- Raspberry Pi 5 exposes PCIe for fast peripherals through a separate M.2 HAT or adapter.
- Raspberry Pi SSD Kit bundles a Raspberry Pi M.2 HAT+ with a Raspberry Pi NVMe SSD.
- Official Raspberry Pi SSD Kit specs:
  <https://www.raspberrypi.com/products/ssd-kit/>.
- Official Raspberry Pi SD Card specs:
  <https://www.raspberrypi.com/products/sd-cards/>.
- Raspberry Pi 27W USB-C Power Supply is the reference PSU because Raspberry Pi recommends a 5V/5A USB-C supply for Pi 5 and high-power peripherals.
- Raspberry Pi Active Cooler is the reference cooler because the Pi 5 performs best with active cooling under sustained heavy load.
