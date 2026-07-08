# Voice Input

The phone cockpit shows a `Mic` soft key for owner sessions. Tap it to start browser speech recognition, speak, then tap `Mic` or `Stop` to end capture. Final transcripts are typed into the PTY without pressing enter.

Onibi uses `SpeechRecognition` when available and falls back to `webkitSpeechRecognition` for Safari and Chromium browsers that still expose the prefixed API. Unsupported browsers keep the soft key visible but show an unavailable state instead of sending audio.

## Permissions

The browser microphone prompt appears after the `Mic` tap. If permission is denied, allow microphone access for the Onibi origin in browser site settings and tap `Mic` again.

Some browsers process speech through a vendor recognition service instead of fully local recognition. Do not dictate secrets unless that is acceptable for your browser and OS configuration.

## Language

The voice overlay includes a language selector. `auto` uses the page language, then the browser language. Specific choices are stored in `localStorage` under `onibi-voice-lang`.

Recognition accuracy depends on browser, OS dictation engine, microphone, network path, accent, and background noise. Treat dictated shell text as draft input and review it in the terminal before submitting commands.

## Browser Notes

[MDN marks `SpeechRecognition` as limited availability](https://developer.mozilla.org/en-US/docs/Web/API/SpeechRecognition). Current Safari and Chromium-family browsers expose enough API surface for Onibi's best-effort dictation path, but Firefox and older WebViews may not. Onibi checks support at runtime.
