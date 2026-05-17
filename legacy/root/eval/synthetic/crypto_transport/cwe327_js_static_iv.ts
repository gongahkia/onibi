import crypto from "crypto";
const cipher = crypto.createCipheriv("aes-256-cbc", key, Buffer.from("0123456789abcdef"));
