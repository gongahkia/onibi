from diagrams import Diagram, Cluster, Edge
from diagrams.custom import Custom
from diagrams.onprem.client import User
from diagrams.onprem.monitoring import Grafana
from diagrams.programming.framework import React
from diagrams.generic.storage import Storage
from diagrams.generic.compute import Rack
from diagrams.generic.os import Ubuntu
from diagrams.generic.blank import Blank
import os

graph_attr = {
    "fontsize": "28",
    "fontname": "Helvetica",
    "pad": "0.8",
    "ranksep": "1.2",
    "nodesep": "0.8",
}
node_attr = {
    "fontsize": "13",
    "fontname": "Helvetica",
}
edge_attr = {
    "fontsize": "11",
    "fontname": "Helvetica",
}
cluster_attr = {
    "fontsize": "16",
    "fontname": "Helvetica Bold",
    "style": "rounded",
    "penwidth": "2",
}

out = os.path.dirname(os.path.abspath(__file__))

with Diagram(
    "Onibi Architecture",
    filename=os.path.join(out, "architecture"),
    outformat="png",
    show=False,
    direction="TB",
    graph_attr=graph_attr,
    node_attr=node_attr,
    edge_attr=edge_attr,
):
    with Cluster("macOS Menubar App", graph_attr={**cluster_attr}):
        with Cluster("Views / UI Layer", graph_attr={**cluster_attr}):
            menubar = Rack("MenuBarController")
            menuview = Rack("MenuBarView")
            settings_view = Rack("SettingsView")

        with Cluster("ViewModels", graph_attr={**cluster_attr}):
            settings_vm = Rack("SettingsViewModel")
            notif_vm = Rack("NotificationListVM")

        with Cluster("Core Services", graph_attr={**cluster_attr}):
            eventbus = Grafana("EventBus\n(Combine)")
            scheduler = Rack("BackgroundTask\nScheduler")
            notif_mgr = Rack("Notification\nManager")
            session_mgr = Rack("Session\nManager")
            err_reporter = Rack("Error\nReporter")
            dep_container = Rack("Dependency\nContainer")

        with Cluster("Log Processing Pipeline", graph_attr={**cluster_attr}):
            file_watcher = Rack("FileWatcher\n(FSEvents)")
            log_buffer = Rack("LogBuffer")
            log_parser = Rack("LogFileParser")
            log_truncator = Rack("LogFile\nTruncator")

        with Cluster("Event Detection", graph_attr={**cluster_attr}):
            ai_detector = Rack("AI Response\nDetector")
            task_detector = Rack("Task Completion\nDetector")
            dev_detector = Rack("Dev Workflow\nParser")
            fp_reducer = Rack("FalsePositive\nReducer")

        with Cluster("Ghostty Integration", graph_attr={**cluster_attr}):
            ipc_client = Rack("GhosttyIPC\nClient")
            cli_service = Rack("GhosttyCli\nService")
            process_mon = Rack("Process\nMonitor")
            shell_hooks = Rack("ShellHook\nInstaller")

        with Cluster("Data / Storage", graph_attr={**cluster_attr}):
            json_storage = Storage("JSONStorage\nManager")
            lru_cache = Storage("LRU Cache")
            user_defaults = Storage("UserDefaults")

        with Cluster("Utilities", graph_attr={**cluster_attr}):
            logger = Rack("Log\n(os.log)")
            perf_monitor = Rack("Performance\nMonitor")
            constants = Rack("Constants")

    ghostty = Ubuntu("Ghostty\nTerminal")
    log_file = Storage("~/.config/onibi/\nterminal.log")
    macos_notif = Rack("macOS\nNotification Center")

    # --- external connections ---
    ghostty >> Edge(label="shell hooks write", color="#e94560") >> log_file
    shell_hooks >> Edge(label="installs hooks", color="#e94560", style="dashed") >> ghostty

    # --- log pipeline ---
    log_file >> Edge(label="FSEvents", color="#0f3460") >> file_watcher
    file_watcher >> Edge(color="#0f3460") >> log_buffer
    log_buffer >> Edge(color="#0f3460") >> log_parser
    log_truncator >> Edge(label="rotate", color="#533483", style="dashed") >> log_file

    # --- scheduler orchestrates pipeline ---
    scheduler >> Edge(color="#e94560") >> file_watcher
    scheduler >> Edge(color="#e94560") >> log_buffer
    scheduler >> Edge(color="#e94560") >> log_parser

    # --- parsed events flow ---
    log_parser >> Edge(color="#0f3460") >> ai_detector
    log_parser >> Edge(color="#0f3460") >> task_detector
    log_parser >> Edge(color="#0f3460") >> dev_detector
    ai_detector >> Edge(color="#533483") >> fp_reducer
    task_detector >> Edge(color="#533483") >> fp_reducer
    dev_detector >> Edge(color="#533483") >> fp_reducer

    # --- eventbus hub ---
    fp_reducer >> Edge(label="publish", color="#e94560") >> eventbus
    scheduler >> Edge(label="events", color="#e94560") >> eventbus
    eventbus >> Edge(color="#e94560") >> notif_mgr
    eventbus >> Edge(color="#e94560") >> session_mgr
    eventbus >> Edge(color="#e94560") >> menubar
    eventbus >> Edge(color="#e94560", style="dashed") >> settings_vm

    # --- notifications ---
    notif_mgr >> Edge(label="UNNotification", color="#533483") >> macos_notif

    # --- ghostty integration ---
    ipc_client >> Edge(color="#0f3460") >> cli_service
    process_mon >> Edge(label="NSWorkspace", color="#0f3460") >> ipc_client

    # --- storage ---
    scheduler >> Edge(label="append", color="#533483", style="dashed") >> json_storage
    scheduler >> Edge(style="dashed", color="#533483") >> lru_cache
    settings_vm >> Edge(color="#533483", style="dashed") >> user_defaults

    # --- UI connections ---
    menubar >> Edge(color="#0f3460") >> menuview
    settings_view >> Edge(color="#0f3460") >> settings_vm

    # --- error reporting ---
    err_reporter >> Edge(label="os.log", color="#533483", style="dashed") >> logger

    # --- DI container ---
    dep_container >> Edge(style="dashed", color="#555577") >> eventbus
    dep_container >> Edge(style="dashed", color="#555577") >> session_mgr
    dep_container >> Edge(style="dashed", color="#555577") >> notif_mgr
    dep_container >> Edge(style="dashed", color="#555577") >> err_reporter