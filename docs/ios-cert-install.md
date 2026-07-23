# iOS Local Certificate Trust

Onibi serves the phone cockpit over local HTTPS. iOS cannot be made to trust a
local CA programmatically, so install and trust the profile printed by
`onibi up` before opening the pair URL. The profile contains the local CA's
public certificate; its private signing key stays on the Mac.

1. Transfer the printed `onibi-local-ca.mobileconfig` from the Mac through a
   channel you control, such as private file sync or a USB cable. Do not
   download a CA profile from the local network.
2. Open the profile on the iPhone and install it.
3. Open Settings.
4. Go to General -> VPN & Device Management and confirm the Onibi profile is installed.
5. Go to General -> About -> Certificate Trust Settings.
6. Enable full trust for `Onibi local CA`.
7. Restart `onibi up` and scan the new QR.

If pairing redirects to `Forbidden`, the phone reached Onibi but did not return
the owner cookie. Trust the local CA first. Use a hotspot only when the phone
cannot reach the pair URL at all.
