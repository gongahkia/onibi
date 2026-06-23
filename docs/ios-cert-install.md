# iOS Local Certificate Trust

Onibi serves the phone cockpit over local HTTPS. iOS cannot be made to trust a
local certificate programmatically, so install and trust the certificate
profile manually before opening the pair URL.

1. Send the Onibi local certificate profile to the iPhone.
2. Open Settings.
3. Go to General -> VPN & Device Management.
4. Select the Onibi profile and tap Install.
5. Go to General -> About -> Certificate Trust Settings.
6. Enable full trust for the Onibi certificate.
7. Reopen the Onibi pair URL.
