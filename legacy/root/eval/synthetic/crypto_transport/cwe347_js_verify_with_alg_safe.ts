import jwt from "jsonwebtoken";
jwt.verify(token, secret, { algorithms: ["HS256"] });
