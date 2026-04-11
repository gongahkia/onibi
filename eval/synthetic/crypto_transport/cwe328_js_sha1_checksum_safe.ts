import crypto from "crypto";
// checksum for downloaded file
const checksum = crypto.createHash("sha1").update(fileContents).digest("hex");
