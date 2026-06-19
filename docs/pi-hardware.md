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

## Source Notes

- Raspberry Pi 5 exposes PCIe for fast peripherals through a separate M.2 HAT or adapter.
- Raspberry Pi SSD Kit bundles a Raspberry Pi M.2 HAT+ with a Raspberry Pi NVMe SSD.
- Raspberry Pi 27W USB-C Power Supply is the reference PSU because Raspberry Pi recommends a 5V/5A USB-C supply for Pi 5 and high-power peripherals.
- Raspberry Pi Active Cooler is the reference cooler because the Pi 5 performs best with active cooling under sustained heavy load.
