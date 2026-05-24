import express from "express";
import axios from "axios";

const app = express();

app.get("/proxy/report", async (req, res) => {
  const reportId = req.query.reportId as string;
  if (!reportId) {
    res.status(400).send("missing reportId");
    return;
  }
  const endpoint = `https://internal.service.local/reports/${reportId}`;
  const response = await axios.get(endpoint);
  res.send(response.data);
});
