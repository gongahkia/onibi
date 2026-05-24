import express from "express";
import { Sequelize, DataTypes, Model } from "sequelize";

const app = express();
const sequelize = new Sequelize(process.env.DATABASE_URL!);

class User extends Model {}
User.init({ email: DataTypes.STRING }, { sequelize, modelName: "user" });

app.get("/users", async (req, res) => {
  const email = req.query.email as string;
  if (!email) {
    res.status(400).send("missing email");
    return;
  }
  const users = await User.findAll({ where: { email } }); // safe: Sequelize model generates parameterized SQL
  res.json(users);
});
