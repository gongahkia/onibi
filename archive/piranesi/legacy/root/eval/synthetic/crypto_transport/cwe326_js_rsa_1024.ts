import crypto from "crypto";
crypto.generateKeyPairSync("rsa", { modulusLength: 1024 });
