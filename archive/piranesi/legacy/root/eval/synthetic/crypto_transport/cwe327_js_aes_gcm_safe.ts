import crypto from "crypto";
const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
