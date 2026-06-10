// Package intake serves the local Unix-domain socket that adapter hooks and
// the onibi-notify client write JSON events to. Permissions 0600, peer-cred
// verified, schema-validated. See TODO §7.3 (T11 mitigation).
package intake
