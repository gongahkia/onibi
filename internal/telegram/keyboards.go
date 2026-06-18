package telegram

import (
	"strings"

	"github.com/go-telegram/bot/models"
)

// Callback data prefixes. Telegram limits callback_data to 64 bytes; our
// ids are 16 hex chars so verb:id stays well under.
const (
	CBApprove         = "approve:"
	CBConfirm         = "confirm:"
	CBDeny            = "deny:"
	CBDenyReason      = "reason:"
	CBEdit            = "edit:"
	CBTarget          = "target:"
	CBPromptSend      = "psend:"
	CBPromptEdit      = "pedit:"
	CBPromptCancel    = "pcancel:"
	CBPromptUp        = "pup:"
	CBPromptDown      = "pdown:"
	CBPeek            = "peek:"
	CBText            = "text:"
	CBRender          = "render:"
	CBLegacyShot      = "shot:"
	CBShow            = "show:"
	CBHide            = "hide:"
	CBHideHeadless    = "hideh:"
	CBHideEnd         = "hidee:"
	CBInterrupt       = "int:"
	CBKill            = "kill:"
	CBMenuStatus      = "mstatus"
	CBMenuSessions    = "msessions"
	CBMenuQueue       = "mqueue"
	CBMenuSecure      = "msecure"
	CBMenuNewHeadless = "mnewh"
	CBMenuNewVisible  = "mnewv"
	CBMenuProjects    = "mproj"
	CBMenuDoctor      = "mdoc"
	CBMenuHooks       = "mhooks"
	CBMenuSend        = "msend:"
	CBMenuSnooze      = "msnooze"
	CBMenuUnsnooze    = "munsnooze"
	CBMenuHome        = "mmenu"
	CBOnboardProject  = "obproj"
	CBOnboardAgent    = "obagent"
	CBOnboardVisible  = "obvis"
	CBOnboardDemo     = "obdemo"
	CBProjectAlias    = "proj:"
	CBProjectStart    = "pnew:"
)

type SessionTarget struct {
	ID       string
	Label    string
	Selected bool
	Visible  bool
}

// ApprovalKeyboard returns the inline keyboard rendered alongside an
// approval request. Three buttons in one row so they stay tappable on a
// phone without horizontal scroll.
func ApprovalKeyboard(approvalID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Approve", CallbackData: CBApprove + approvalID},
				{Text: "Deny", CallbackData: CBDeny + approvalID},
				{Text: "Edit", CallbackData: CBEdit + approvalID},
			},
			{
				{Text: "Reason", CallbackData: CBDenyReason + approvalID},
			},
		},
	}
}

func ConfirmApprovalKeyboard(approvalID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Confirm approve", CallbackData: CBConfirm + approvalID},
			},
			{
				{Text: "Deny", CallbackData: CBDeny + approvalID},
				{Text: "Reason", CallbackData: CBDenyReason + approvalID},
				{Text: "Edit", CallbackData: CBEdit + approvalID},
			},
		},
	}
}

func EncryptedApprovalKeyboard(url string) *models.ReplyKeyboardMarkup {
	return EncryptedWebAppKeyboard("Open encrypted approval", url)
}

func EncryptedItemKeyboard(url string) *models.ReplyKeyboardMarkup {
	return EncryptedWebAppKeyboard("Open encrypted item", url)
}

func SecureComposerKeyboard(url string) *models.ReplyKeyboardMarkup {
	return EncryptedWebAppKeyboard("Open secure controls", url)
}

func EncryptedWebAppKeyboard(label, url string) *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{{Text: label, WebApp: &models.WebAppInfo{URL: url}}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func OnboardingKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: "Add Project", CallbackData: CBOnboardProject},
			{Text: "Choose Agent", CallbackData: CBOnboardAgent},
		},
		{
			{Text: "Start Visible", CallbackData: CBOnboardVisible},
			{Text: "Test Approval", CallbackData: CBOnboardDemo},
		},
		{
			{Text: "Sessions", CallbackData: CBMenuSessions},
			{Text: "Menu", CallbackData: CBMenuHome},
		},
	}}
}

// DecidedKeyboard replaces the approval keyboard after a decision lands,
// leaving a single non-interactive label so the user can see at a glance
// what state the row ended in.
func DecidedKeyboard(label string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: label, CallbackData: "noop"}},
		},
	}
}

func SessionTargetKeyboard(targets []SessionTarget) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(targets))
	for _, t := range targets {
		label := t.Label
		if label == "" {
			label = t.ID
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         trimButton(label),
			CallbackData: CBTarget + t.ID,
		}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func ProjectAliasKeyboard(aliases []string) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(aliases))
	for _, alias := range aliases {
		if len(CBProjectAlias+alias) > 64 {
			continue
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         trimButton(alias),
			CallbackData: CBProjectAlias + alias,
		}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func ProjectStartKeyboard(alias string) *models.InlineKeyboardMarkup {
	if len(CBProjectStart+"headless:codex:"+alias) > 64 {
		return &models.InlineKeyboardMarkup{}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: "Visible shell", CallbackData: CBProjectStart + "visible:shell:" + alias},
			{Text: "Headless shell", CallbackData: CBProjectStart + "headless:shell:" + alias},
		},
		{
			{Text: "Visible codex", CallbackData: CBProjectStart + "visible:codex:" + alias},
			{Text: "Headless codex", CallbackData: CBProjectStart + "headless:codex:" + alias},
		},
	}}
}

func PromptKeyboard(id string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Send now", CallbackData: CBPromptSend + id},
				{Text: "Edit", CallbackData: CBPromptEdit + id},
				{Text: "Cancel", CallbackData: CBPromptCancel + id},
			},
			{
				{Text: "Up", CallbackData: CBPromptUp + id},
				{Text: "Down", CallbackData: CBPromptDown + id},
			},
		},
	}
}

func SessionMenuKeyboard(targets []SessionTarget) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(targets)*2+5)
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Status", CallbackData: CBMenuStatus},
		{Text: "Sessions", CallbackData: CBMenuSessions},
		{Text: "Queue", CallbackData: CBMenuQueue},
		{Text: "Secure", CallbackData: CBMenuSecure},
	})
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "New Visible", CallbackData: CBMenuNewVisible},
		{Text: "New Headless", CallbackData: CBMenuNewHeadless},
		{Text: "Projects", CallbackData: CBMenuProjects},
		{Text: "Test Approval", CallbackData: CBOnboardDemo},
	})
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Snooze", CallbackData: CBMenuSnooze},
		{Text: "Unsnooze", CallbackData: CBMenuUnsnooze},
	})
	for _, t := range targets {
		label := t.Label
		if label == "" {
			label = t.ID
		}
		if t.Selected {
			label = "* " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: trimButton(label), CallbackData: CBTarget + t.ID}})
		if t.Selected {
			showHideText := "Show"
			showHideData := CBShow + t.ID
			if t.Visible {
				showHideText = "Hide"
				showHideData = CBHide + t.ID
			}
			rows = append(rows, []models.InlineKeyboardButton{
				{Text: "Peek", CallbackData: CBPeek + t.ID},
				{Text: "Send", CallbackData: CBMenuSend + t.ID},
				{Text: "Interrupt", CallbackData: CBInterrupt + t.ID},
				{Text: showHideText, CallbackData: showHideData},
			})
		}
	}
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Doctor", CallbackData: CBMenuDoctor},
		{Text: "Hooks", CallbackData: CBMenuHooks},
	})
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func HideChoiceKeyboard(id string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: "Continue headless", CallbackData: CBHideHeadless + id},
			{Text: "End session", CallbackData: CBHideEnd + id},
		},
	}}
}

// ParseCallback splits a callback_data string into (verb, approvalID).
// Returns ("", "") for unknown verbs so callers can ignore.
func ParseCallback(data string) (verb, id string) {
	switch {
	case data == "noop":
		return "noop", ""
	case data == CBMenuStatus:
		return "menu_status", ""
	case data == CBMenuSessions:
		return "menu_sessions", ""
	case data == CBMenuQueue:
		return "menu_queue", ""
	case data == CBMenuSecure:
		return "menu_secure", ""
	case data == CBMenuNewHeadless:
		return "menu_new_headless", ""
	case data == CBMenuNewVisible:
		return "menu_new_visible", ""
	case data == CBMenuProjects:
		return "menu_projects", ""
	case data == CBMenuDoctor:
		return "menu_doctor", ""
	case data == CBMenuHooks:
		return "menu_hooks", ""
	case data == CBMenuSnooze:
		return "menu_snooze", ""
	case data == CBMenuUnsnooze:
		return "menu_unsnooze", ""
	case data == CBMenuHome:
		return "menu_home", ""
	case data == CBOnboardProject:
		return "onboard_project", ""
	case data == CBOnboardAgent:
		return "onboard_agent", ""
	case data == CBOnboardVisible:
		return "onboard_visible", ""
	case data == CBOnboardDemo:
		return "demo_approval", ""
	case strings.HasPrefix(data, CBProjectAlias):
		return "project_alias", strings.TrimPrefix(data, CBProjectAlias)
	case strings.HasPrefix(data, CBProjectStart):
		return "project_start", strings.TrimPrefix(data, CBProjectStart)
	case strings.HasPrefix(data, CBApprove):
		return "approve", strings.TrimPrefix(data, CBApprove)
	case strings.HasPrefix(data, CBConfirm):
		return "confirm_approve", strings.TrimPrefix(data, CBConfirm)
	case strings.HasPrefix(data, CBDeny):
		return "deny", strings.TrimPrefix(data, CBDeny)
	case strings.HasPrefix(data, CBDenyReason):
		return "deny_reason", strings.TrimPrefix(data, CBDenyReason)
	case strings.HasPrefix(data, CBEdit):
		return "edit", strings.TrimPrefix(data, CBEdit)
	case strings.HasPrefix(data, CBTarget):
		return "target", strings.TrimPrefix(data, CBTarget)
	case strings.HasPrefix(data, CBPromptSend):
		return "prompt_send", strings.TrimPrefix(data, CBPromptSend)
	case strings.HasPrefix(data, CBPromptEdit):
		return "prompt_edit", strings.TrimPrefix(data, CBPromptEdit)
	case strings.HasPrefix(data, CBPromptCancel):
		return "prompt_cancel", strings.TrimPrefix(data, CBPromptCancel)
	case strings.HasPrefix(data, CBPromptUp):
		return "prompt_up", strings.TrimPrefix(data, CBPromptUp)
	case strings.HasPrefix(data, CBPromptDown):
		return "prompt_down", strings.TrimPrefix(data, CBPromptDown)
	case strings.HasPrefix(data, CBMenuSend):
		return "menu_send", strings.TrimPrefix(data, CBMenuSend)
	case strings.HasPrefix(data, CBPeek):
		return "peek", strings.TrimPrefix(data, CBPeek)
	case strings.HasPrefix(data, CBText):
		return "text", strings.TrimPrefix(data, CBText)
	case strings.HasPrefix(data, CBRender):
		return "render", strings.TrimPrefix(data, CBRender)
	case strings.HasPrefix(data, CBLegacyShot):
		return "render", strings.TrimPrefix(data, CBLegacyShot)
	case strings.HasPrefix(data, CBShow):
		return "show", strings.TrimPrefix(data, CBShow)
	case strings.HasPrefix(data, CBHide):
		return "hide", strings.TrimPrefix(data, CBHide)
	case strings.HasPrefix(data, CBHideHeadless):
		return "hide_headless", strings.TrimPrefix(data, CBHideHeadless)
	case strings.HasPrefix(data, CBHideEnd):
		return "hide_end", strings.TrimPrefix(data, CBHideEnd)
	case strings.HasPrefix(data, CBInterrupt):
		return "interrupt", strings.TrimPrefix(data, CBInterrupt)
	case strings.HasPrefix(data, CBKill):
		return "kill", strings.TrimPrefix(data, CBKill)
	}
	return "", ""
}

func trimButton(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:37]) + "..."
}
