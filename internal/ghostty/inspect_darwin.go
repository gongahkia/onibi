//go:build darwin

package ghostty

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

// AppKit metadata queries only. There is deliberately no Application() proxy,
// Apple event, activation, terminal enumeration, or Accessibility text read.
const inspectionScript = `ObjC.import('AppKit');
function run() {
 var apps=$.NSRunningApplication.runningApplicationsWithBundleIdentifier('com.mitchellh.ghostty');
 var count=Number(apps.count);
 var url=count===1 ? apps.objectAtIndex(0).bundleURL : $.NSWorkspace.sharedWorkspace.URLForApplicationWithBundleIdentifier('com.mitchellh.ghostty');
 var installed=Boolean(url && !url.isNil());
 var version='';
 if(installed) { var value=$.NSBundle.bundleWithURL(url).objectForInfoDictionaryKey('CFBundleShortVersionString'); if(value && !value.isNil()) version=String(ObjC.unwrap(value)).slice(0,64); }
 return JSON.stringify({installed:installed,running:count,version:version});
}`

func inspectNative(ctx context.Context) (environment, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", inspectionScript)
	var out boundedOutput
	out.limit = 4096
	cmd.Stdout = &out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return environment{}, errors.New("app metadata unavailable")
	}
	var env environment
	if out.overflow || json.Unmarshal(out.buf.Bytes(), &env) != nil || env.Running < 0 || len(env.Version) > 64 {
		return environment{}, errors.New("invalid app metadata")
	}
	return env, nil
}
