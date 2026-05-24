from __future__ import annotations

from pathlib import Path

from piranesi.detect.php_patterns import extract_php_pattern_findings


def _write_file(tmp_path: Path, name: str, source: str) -> Path:
    path = tmp_path / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(source, encoding="utf-8")
    return path


def _scan(tmp_path: Path, *files: Path):
    return extract_php_pattern_findings(tmp_path, files=files)


def _findings_for_cwe(findings, cwe_id: str):
    return [finding for finding in findings if finding.vuln_class == cwe_id]


def test_extract_post_detected_as_mass_assignment(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "extract_post.php",
        """<?php
extract($_POST);
if ($is_admin) {
    grant_admin();
}
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-915")


def test_variable_variables_with_request_taint_are_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "variable_variables.php",
        """<?php
$field = $_GET['field'];
$$field = $_GET['value'];
echo $name;
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-473")


def test_loose_zero_compare_is_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "type_juggling.php",
        """<?php
if ($_GET['token'] == 0) {
    grant_access();
}
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-1289")


def test_unserialize_with_destructor_gadget_is_high_confidence(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "gadget_chain.php",
        """<?php
class DeleteCache {
    public function __destruct() {
        unlink('/tmp/cache');
    }
}

unserialize($_COOKIE['payload']);
""",
    )

    findings = _scan(tmp_path, source)
    cwe_findings = _findings_for_cwe(findings, "CWE-502")

    assert cwe_findings
    assert any(finding.metadata.get("gadget_chain") is True for finding in cwe_findings)
    assert any(
        "__destruct" in finding.metadata.get("magic_methods", []) for finding in cwe_findings
    )


def test_laravel_guarded_empty_is_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "app/Models/User.php",
        """<?php
use Illuminate\\Database\\Eloquent\\Model;

class User extends Model {
    protected $guarded = [];
}
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-915")


def test_blade_form_without_csrf_is_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "resources/views/edit.blade.php",
        """<form method="POST" action="/profile/update">
    <input type="text" name="name">
</form>
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-352")


def test_blade_form_with_csrf_is_not_reported(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "resources/views/edit_safe.blade.php",
        """<form method="POST" action="/profile/update">
    @csrf
    <input type="text" name="name">
</form>
""",
    )

    findings = _scan(tmp_path, source)

    assert not _findings_for_cwe(findings, "CWE-352")


def test_symfony_security_yaml_anonymous_admin_access_is_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "config/packages/security.yaml",
        """security:
  firewalls:
    main:
      pattern: ^/
  access_control:
    - { path: ^/admin, roles: IS_AUTHENTICATED_ANONYMOUSLY }
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-306")


def test_symfony_voter_that_always_grants_is_detected(tmp_path: Path) -> None:
    source = _write_file(
        tmp_path,
        "src/Security/PostVoter.php",
        """<?php
use Symfony\\Component\\Security\\Core\\Authorization\\Voter\\Voter;
use Symfony\\Component\\Security\\Core\\Authorization\\Voter\\VoterInterface;

class PostVoter extends Voter {
    protected function voteOnAttribute(string $attribute, mixed $subject, $token): bool {
        return VoterInterface::ACCESS_GRANTED;
    }
}
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-269")


def test_wordpress_rest_route_with_return_true_permission_callback_is_detected(
    tmp_path: Path,
) -> None:
    source = _write_file(
        tmp_path,
        "wp-content/plugins/open-api/plugin.php",
        """<?php
register_rest_route('demo/v1', '/flush', [
    'methods' => 'POST',
    'callback' => 'flush_cache',
    'permission_callback' => '__return_true',
]);
""",
    )

    findings = _scan(tmp_path, source)

    assert _findings_for_cwe(findings, "CWE-306")
