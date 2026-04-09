import express from "express";
import { PrismaClient } from "@prisma/client";

const app = express();
app.use(express.json());
const prisma = new PrismaClient();

app.post("/search-users", async (req, res) => {
  const email = req.body.email as string;
  if (!email) {
    res.status(400).send("missing email");
    return;
  }
  const users = await prisma.user.findMany({ // safe: Prisma generates parameterized SQL
    where: { email },
  });
  res.json(users);
});
