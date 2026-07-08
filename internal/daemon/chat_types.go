package daemon

type slackApprovalRef struct {
	Channel string
	TS      string
}

type discordApprovalRef struct {
	Channel string
	Message string
}
