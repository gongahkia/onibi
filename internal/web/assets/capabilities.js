"use strict";

const capabilityAvailability = {
  available: "Available in Onibi",
  version_supported: "Version supports this API",
  version_unknown: "Ghostty version unknown",
  version_blocked: "Needs a newer Ghostty version",
  not_implemented: "Not implemented",
};
async function loadCapabilities() {
  const generation = state.authGeneration;
  byId("refresh-capabilities").disabled = true;
  byId("capability-message").textContent = "Loading capability details…";
  try {
    const report = await api("/api/capabilities");
    if (generation !== state.authGeneration) return;
    byId("capability-summary").textContent =
      "Ghostty " +
      (report.version?.version || "version unknown") +
      (report.nativeBackend
        ? " · Your Mac’s native adapter"
        : " · Simulated backend; native verification is unavailable");
    byId("capability-list").replaceChildren();
    const groups = new Map();
    for (const feature of report.features || []) {
      if (!groups.has(feature.group)) {
        const section = document.createElement("details");
        const summary = document.createElement("summary");
        summary.textContent = feature.group;
        section.append(summary);
        section.className = "capability-group";
        if (groups.size === 0) section.open = true;
        const list = document.createElement("ul");
        list.className = "health-checks";
        section.append(list);
        groups.set(feature.group, list);
        byId("capability-list").append(section);
      }
      const row = document.createElement("li");
      const title = document.createElement("strong");
      title.textContent = feature.name;
      const detail = document.createElement("p");
      detail.textContent = feature.detail;
      const status = document.createElement("p");
      status.className = "capability-status";
      status.textContent = feature.implemented
        ? "Implemented · " +
          (feature.automated
            ? "Automated contract coverage"
            : "Automated coverage pending")
        : "Not implemented";
      const requirement = document.createElement("p");
      requirement.className = "muted";
      requirement.textContent =
        capabilityAvailability[feature.availability] || "Availability unknown";
      if (feature.minimumVersion)
        requirement.textContent +=
          " · Requires Ghostty " + feature.minimumVersion + "+";
      if (feature.requiresFocus)
        requirement.textContent += " · Focused pane required";
      const evidence = document.createElement("p");
      evidence.className = "muted";
      const verification = {
        observed: "Native read observed",
        scope_removal_observed: "Reviewed pane removal observed",
        not_verified: "Native effect not verified",
        not_applicable: "Native verification not applicable",
      };
      evidence.textContent =
        verification[feature.nativeVerification] ||
        "Native effect not verified";
      if (feature.observation)
        evidence.textContent +=
          " · Last result: " +
          feature.observation.outcome.replaceAll("_", " ") +
          " at " +
          new Date(feature.observation.at).toLocaleString();
      row.append(title, status, detail, requirement, evidence);
      groups.get(feature.group).append(row);
    }
    byId("capability-message").textContent =
      "Input and keys work with agents, editors and shells. Agent task and approval states are not inferred.";
  } catch (error) {
    if (generation === state.authGeneration)
      byId("capability-message").textContent = error.message;
  } finally {
    byId("refresh-capabilities").disabled = false;
  }
}
byId("open-capabilities").addEventListener("click", () => {
  byId("capability-dialog").showModal();
  loadCapabilities();
});
byId("close-capabilities").addEventListener("click", () =>
  byId("capability-dialog").close(),
);
byId("refresh-capabilities").addEventListener("click", loadCapabilities);
