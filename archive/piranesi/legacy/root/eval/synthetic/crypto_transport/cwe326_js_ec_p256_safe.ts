import crypto from "crypto";
crypto.generateKeyPairSync("ec", { namedCurve: "prime256v1" });
