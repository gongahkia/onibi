// Static JXA bridge. All request data comes from stdin, never generated source.
ObjC.import('AppKit');

function run() {
    var attempted = false;
    function failure(code) {
        return JSON.stringify({status: attempted ? 'uncertain' : 'rejected', code: code});
    }
    try {
        var data = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
        var req = JSON.parse(ObjC.unwrap($.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding)));
        var applications = $.NSRunningApplication.runningApplicationsWithBundleIdentifier('com.mitchellh.ghostty');
        if (Number(applications.count) !== 1) return failure('not_running');
        var process = applications.objectAtIndex(0);
        var pid = Number(process.processIdentifier);
        var started = String(process.launchDate.timeIntervalSince1970);
        if (req.pid && (req.pid !== pid || req.started !== started)) return failure('stale_process');
        var app = Application('com.mitchellh.ghostty');
        // Force a read before considering any mutation; this surfaces disabled
        // scripting and Automation denial without an uncertain input attempt.
        var windows = app.windows();
        var focusedID = '';
        try { focusedID = app.frontWindow().selectedTab().focusedTerminal().id(); } catch (_) {}
        function success(extra) {
            var value = {status:'ok', pid:pid, started:started};
            Object.keys(extra || {}).forEach(function(k) { value[k] = extra[k]; });
            return JSON.stringify(value);
        }
        function findWindow(id) {
            for (var i=0; i<windows.length; i++) if (windows[i].id()===id) return windows[i];
            return null;
        }
        function findTab(window, id) {
            var tabs=window.tabs();
            for (var i=0; i<tabs.length; i++) if (tabs[i].id()===id) return tabs[i];
            return null;
        }
        function findTerminal(id) {
            var all=app.terminals();
            for (var i=0; i<all.length; i++) if (all[i].id()===id) return all[i];
            return null;
        }
        function bounded(value, limit) { return String(value || '').slice(0, limit); }
        if (req.operation==='targets') {
            var targets=[];
            for (var wi=0; wi<windows.length; wi++) {
                var window=windows[wi], windowID=window.id(), windowTitle=bounded(window.name(),512);
                var tabs=window.tabs();
                for(var ti=0; ti<tabs.length; ti++) {
                    var tab=tabs[ti], tabID=tab.id(), tabTitle=bounded(tab.name(),512), terminals=tab.terminals();
                    for(var si=0; si<terminals.length; si++) {
                        var terminal=terminals[si], id=terminal.id();
                        if(targets.length>=256) return failure('too_many_targets');
                        targets.push({id:id, windowId:windowID, windowTitle:windowTitle, tabId:tabID, tabTitle:tabTitle, title:bounded(terminal.name(),512), cwd:bounded(terminal.workingDirectory(),2048), focused:id===focusedID});
                    }
                }
            }
            return success({targets:targets});
        }
        if (req.operation==='new_tab' || req.operation==='close_tab') {
            var window=findWindow(req.windowId);
            if (!window) return failure('missing_window');
            if(req.operation==='new_tab') {
                attempted=true;
                var created=app.newTab({'in':window});
                return success({targetId:created.focusedTerminal().id()});
            }
            var tab=findTab(window,req.tabId);
            if(!tab) return failure('missing_tab');
            attempted=true;
            app.closeTab(tab);
            return success();
        }
        var terminal=findTerminal(req.targetId);
        if(!terminal) return failure('missing_target');
        if(req.operation==='focused') {
            if(focusedID!==req.targetId) return failure('focus_required');
            return success({targetId:req.targetId});
        }
        if(req.operation==='focus') {
            attempted=true;
            app.focus(terminal);
            // Ghostty queues focus on its main thread. A successful Apple
            // event alone does not establish that the requested pane won.
            var focused=false;
            for(var attempt=0; attempt<20; attempt++) {
                if(app.frontWindow().selectedTab().focusedTerminal().id()===req.targetId) { focused=true; break; }
                delay(0.05);
            }
            if(!focused) return failure('action_failed');
        } else if(req.operation==='input') {
            attempted=true;
            app.inputText(req.text, {'to':terminal});
            if(req.enter) app.sendKey('enter', {'to':terminal});
        } else if(req.operation==='key') {
            attempted=true;
            app.sendKey(req.key, {'to':terminal, modifiers:req.modifiers || ''});
        } else return failure('query_failed');
        return success({targetId:req.targetId});
    } catch(error) {
        if (!attempted && error.errorNumber===-1743) return failure('permission');
        return failure(attempted ? 'action_failed' : 'query_failed');
    }
}
