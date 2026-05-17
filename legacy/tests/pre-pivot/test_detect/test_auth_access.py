from __future__ import annotations

from pathlib import Path

from piranesi.detect.auth_access import extract_auth_access_findings


def _write_file(tmp_path: Path, name: str, source: str) -> Path:
    path = tmp_path / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(source, encoding="utf-8")
    return path


def _scan(tmp_path: Path, *files: Path):
    return extract_auth_access_findings(tmp_path, files=files)


def _findings_for_cwe(findings, cwe_id: str):
    return [finding for finding in findings if finding.vuln_class == cwe_id]


class TestCSRFDetection:
    def test_express_post_no_csrf(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "app.ts",
            """
import express from "express";

const app = express();
app.post("/transfer", (req, res) => {
  transferFunds(req.body.to, req.body.amount);
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-352")

    def test_express_post_with_csurf_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "app.ts",
            """
import express from "express";
import csurf from "csurf";

const app = express();
app.use(csurf());
app.post("/transfer", (req, res) => {
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-352")

    def test_django_csrf_exempt(self, tmp_path: Path) -> None:
        view = _write_file(
            tmp_path,
            "views.py",
            """
from django.views.decorators.csrf import csrf_exempt

@csrf_exempt
def transfer(request):
    return None
""",
        )

        findings = _scan(tmp_path, view)

        assert _findings_for_cwe(findings, "CWE-352")

    def test_flask_no_csrf_protect(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "app.py",
            """
from flask import Flask, request

app = Flask(__name__)

@app.route("/transfer", methods=["POST"])
def transfer():
    return request.form["amount"]
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-352")

    def test_spring_csrf_disable(self, tmp_path: Path) -> None:
        config = _write_file(
            tmp_path,
            "SecurityConfig.java",
            """
import org.springframework.context.annotation.Configuration;

@Configuration
public class SecurityConfig {
    void configure(HttpSecurity http) throws Exception {
        http.csrf().disable();
    }
}
""",
        )

        findings = _scan(tmp_path, config)

        assert _findings_for_cwe(findings, "CWE-352")


class TestIDORDetection:
    def test_express_params_to_db_no_ownership(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "idor.ts",
            """
import express from "express";

const app = express();
app.get("/api/orders/:id", async (req, res) => {
  const order = await Order.findById(req.params.id);
  res.json(order);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-639")

    def test_express_params_to_db_with_user_check_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "idor_safe.ts",
            """
import express from "express";

const app = express();
app.get("/api/orders/:id", async (req, res) => {
  const order = await Order.findOne({
    where: { id: req.params.id, userId: req.user.id },
  });
  res.json(order);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-639")

    def test_django_pk_no_user_filter(self, tmp_path: Path) -> None:
        view = _write_file(
            tmp_path,
            "views.py",
            """
from django.http import JsonResponse

def order_detail(request, pk):
    order = Order.objects.get(pk=pk)
    return JsonResponse({"id": order.id})
""",
        )

        findings = _scan(tmp_path, view)

        assert _findings_for_cwe(findings, "CWE-639")

    def test_django_pk_with_request_user_no_finding(self, tmp_path: Path) -> None:
        view = _write_file(
            tmp_path,
            "views.py",
            """
from django.http import JsonResponse

def order_detail(request, pk):
    order = Order.objects.get(pk=pk, user=request.user)
    return JsonResponse({"id": order.id})
""",
        )

        findings = _scan(tmp_path, view)

        assert not _findings_for_cwe(findings, "CWE-639")

    def test_spring_pathvariable_no_principal(self, tmp_path: Path) -> None:
        controller = _write_file(
            tmp_path,
            "OrderController.java",
            """
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;

public class OrderController {
    @GetMapping("/orders/{id}")
    public Order getOrder(@PathVariable Long id) {
        return orderRepository.findById(id).orElseThrow();
    }
}
""",
        )

        findings = _scan(tmp_path, controller)

        assert _findings_for_cwe(findings, "CWE-639")


class TestBrokenAuth:
    def test_timing_unsafe_password_compare(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "auth.ts",
            """
function authenticate(user, providedPassword) {
  if (user.password === providedPassword) {
    return true;
  }
  return false;
}
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")

    def test_timing_safe_equal_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "auth_safe.ts",
            """
import crypto from "crypto";

function authenticate(user, providedPassword) {
  return crypto.timingSafeEqual(
    Buffer.from(user.password),
    Buffer.from(providedPassword),
  );
}
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-287")

    def test_jwt_alg_none(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "jwt_none.ts",
            """
import jwt from "jsonwebtoken";

function verify(token, secret) {
  return jwt.verify(token, secret, { algorithms: ["none"] });
}
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")

    def test_jwt_missing_expiry(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "jwt_exp.ts",
            """
import jwt from "jsonwebtoken";

function issue(payload, secret) {
  return jwt.sign(payload, secret);
}
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")

    def test_jwt_with_expiry_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "jwt_exp_safe.ts",
            """
import jwt from "jsonwebtoken";

function issue(payload, secret) {
  return jwt.sign(payload, secret, { expiresIn: "1h", audience: "users" });
}
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-287")

    def test_cookie_missing_samesite_flag(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "cookie.ts",
            """
app.use(session({
  cookie: {
    secure: true,
    httpOnly: true,
  },
}));
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")

    def test_cookie_with_flags_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "cookie_safe.ts",
            """
app.use(session({
  cookie: {
    secure: true,
    httpOnly: true,
    sameSite: "lax",
  },
}));
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-287")

    def test_passport_plaintext(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "passport.ts",
            """
import { LocalStrategy } from "passport-local";

passport.use(new LocalStrategy((username, password, done) => {
  if (password === user.password) {
    return done(null, user);
  }
  return done(null, false);
}));
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")

    def test_django_raw_password(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "views.py",
            """
def login_view(request):
    user = User.objects.get(username=request.POST["username"])
    if user.password == request.POST["password"]:
        login(request, user)
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-287")


class TestSessionFixation:
    def test_express_login_no_regenerate(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "login.ts",
            """
import express from "express";

const app = express();
app.post("/login", (req, res) => {
  const user = authenticate(req.body.username, req.body.password);
  if (user) {
    req.session.userId = user.id;
    res.redirect("/dashboard");
  }
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-384")

    def test_express_login_with_regenerate_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "login_safe.ts",
            """
import express from "express";

const app = express();
app.post("/login", (req, res) => {
  const user = authenticate(req.body.username, req.body.password);
  if (user) {
    req.session.regenerate(() => {
      req.session.userId = user.id;
      res.redirect("/dashboard");
    });
  }
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-384")

    def test_flask_login_no_clear(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "app.py",
            """
from flask import Flask, session

app = Flask(__name__)

@app.route("/login", methods=["POST"])
def login():
    session["user_id"] = 1
    return "ok"
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-384")

    def test_django_manual_session_no_cycle(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "views.py",
            """
def login_view(request):
    request.session["user_id"] = user.id
    return None
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-384")

    def test_spring_session_fixation_none(self, tmp_path: Path) -> None:
        config = _write_file(
            tmp_path,
            "SecurityConfig.java",
            """
public class SecurityConfig {
    void configure(HttpSecurity http) throws Exception {
        http.sessionManagement().sessionFixation().none();
    }
}
""",
        )

        findings = _scan(tmp_path, config)

        assert _findings_for_cwe(findings, "CWE-384")


class TestMassAssignment:
    def test_sequelize_create_req_body(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "mass.ts",
            """
app.post("/users", async (req, res) => {
  const user = await User.create(req.body);
  res.json(user);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-915")

    def test_sequelize_create_destructured_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "mass_safe.ts",
            """
app.post("/users", async (req, res) => {
  const { name, email } = req.body;
  const user = await User.create({ name, email });
  res.json(user);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-915")

    def test_mongoose_new_model_req_body(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "mongoose.ts",
            """
app.post("/users", async (req, res) => {
  const user = new User(req.body);
  await user.save();
  res.json(user);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-915")

    def test_prisma_create_data_req_body(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "prisma.ts",
            """
app.post("/users", async (req, res) => {
  const user = await prisma.user.create({
    data: req.body,
  });
  res.json(user);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-915")

    def test_django_fields_all(self, tmp_path: Path) -> None:
        form = _write_file(
            tmp_path,
            "forms.py",
            """
class UserForm(ModelForm):
    class Meta:
        model = User
        fields = "__all__"
""",
        )

        findings = _scan(tmp_path, form)

        assert _findings_for_cwe(findings, "CWE-915")

    def test_spring_entity_request_body(self, tmp_path: Path) -> None:
        entity = _write_file(
            tmp_path,
            "User.java",
            """
import jakarta.persistence.Entity;

@Entity
public class User {
    private Boolean isAdmin;
}
""",
        )
        controller = _write_file(
            tmp_path,
            "UserController.java",
            """
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;

public class UserController {
    @PostMapping("/users")
    public User createUser(@RequestBody User user) {
        return userRepository.save(user);
    }
}
""",
        )

        findings = _scan(tmp_path, entity, controller)

        assert _findings_for_cwe(findings, "CWE-915")


class TestPrivilegeEscalation:
    def test_admin_route_no_middleware(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "admin.ts",
            """
app.delete("/admin/users/:id", async (req, res) => {
  await User.destroy({ where: { id: req.params.id } });
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-269")

    def test_admin_route_with_auth_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "admin_safe.ts",
            """
app.delete("/admin/users/:id", requireAdmin, async (req, res) => {
  await User.destroy({ where: { id: req.params.id } });
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-269")

    def test_nestjs_controller_no_roles(self, tmp_path: Path) -> None:
        controller = _write_file(
            tmp_path,
            "admin.controller.ts",
            """
@Controller("admin")
export class AdminController {
  @Delete("users/:id")
  deleteUser() {
    return true;
  }
}
""",
        )

        findings = _scan(tmp_path, controller)

        assert _findings_for_cwe(findings, "CWE-269")

    def test_django_admin_view_no_permission(self, tmp_path: Path) -> None:
        view = _write_file(
            tmp_path,
            "views.py",
            """
def admin_delete_user(request, user_id):
    User.objects.get(id=user_id).delete()
""",
        )

        findings = _scan(tmp_path, view)

        assert _findings_for_cwe(findings, "CWE-269")


class TestMissingAuth:
    def test_payment_endpoint_no_auth(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "payment.ts",
            """
app.post("/payment", async (req, res) => {
  await chargeCard(req.body.amount);
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-306")

    def test_delete_user_no_auth(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "delete_user.ts",
            """
app.delete("/users/:id", async (req, res) => {
  await User.destroy({ where: { id: req.params.id } });
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert _findings_for_cwe(findings, "CWE-306")

    def test_payment_endpoint_with_auth_no_finding(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "payment_safe.ts",
            """
app.post("/payment", requireAuth, async (req, res) => {
  await chargeCard(req.body.amount);
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-306")

    def test_public_route_excluded(self, tmp_path: Path) -> None:
        app = _write_file(
            tmp_path,
            "public.ts",
            """
app.post("/reset-password", async (req, res) => {
  res.sendStatus(204);
});
""",
        )

        findings = _scan(tmp_path, app)

        assert not _findings_for_cwe(findings, "CWE-306")

    def test_spring_charge_without_preauthorize(self, tmp_path: Path) -> None:
        controller = _write_file(
            tmp_path,
            "PaymentController.java",
            """
import org.springframework.web.bind.annotation.PostMapping;

public class PaymentController {
    @PostMapping("/charge")
    public void chargeCard() {
        processPayment();
    }
}
""",
        )

        findings = _scan(tmp_path, controller)

        assert _findings_for_cwe(findings, "CWE-306")
