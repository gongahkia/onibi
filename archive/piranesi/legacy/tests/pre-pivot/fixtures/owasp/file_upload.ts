import express from "express";
import multer from "multer";
import fs from "fs";
import path from "path";

const app = express();
const upload = multer({ dest: "uploads/" });

// VULNERABLE: file uploaded with original name, no extension check
app.post("/upload", upload.single("file"), (req, res) => {
  const originalName = req.file!.originalname; // tainted
  const destPath = path.join("uploads/", originalName); // CWE-434 sink
  fs.writeFileSync(destPath, req.file!.buffer); // CWE-434 sink
  res.json({ path: destPath });
});

// VULNERABLE: filename used directly in path concatenation
app.post("/avatar", upload.single("avatar"), (req, res) => {
  const filename = req.file!.originalname; // tainted
  const dest = `public/avatars/${filename}`; // CWE-434 path concat
  fs.renameSync(req.file!.path, dest);
  res.json({ url: `/avatars/${filename}` });
});

// SAFE: extension validation
app.post("/safe-upload", upload.single("doc"), (req, res) => {
  const ext = path.extname(req.file!.originalname).toLowerCase(); // sanitizer
  const allowed = [".pdf", ".docx", ".txt"];
  if (!allowed.includes(ext)) {
    return res.status(400).send("invalid file type");
  }
  const safeName = `${Date.now()}${ext}`;
  fs.writeFileSync(path.join("uploads/", safeName), req.file!.buffer);
  res.json({ ok: true });
});

// SAFE: multer with fileFilter
const filteredUpload = multer({
  dest: "uploads/",
  fileFilter: (req, file, cb) => {
    if (file.mimetype.startsWith("image/")) {
      cb(null, true);
    } else {
      cb(new Error("only images allowed"));
    }
  },
});
app.post("/safe-avatar", filteredUpload.single("avatar"), (req, res) => {
  res.json({ ok: true });
});
