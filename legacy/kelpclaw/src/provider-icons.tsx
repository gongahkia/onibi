import { Cable, Database, Mail, Table2, Webhook } from "lucide-react";
import type { ComponentType, SVGProps } from "react";
import type { LucideIcon } from "lucide-react";

type ProviderIconProps = {
  readonly provider: string | undefined;
  readonly size?: number | undefined;
  readonly className?: string | undefined;
  readonly title?: string | undefined;
};

type SvgIconProps = SVGProps<SVGSVGElement> & {
  readonly size?: number | undefined;
  readonly title?: string | undefined;
};

const adapterProviderIcons: Readonly<Record<string, string>> = {
  "adapter.gmail": "gmail",
  "adapter.sheets": "sheets",
  "adapter.email": "smtp",
  "adapter.whatsapp": "whatsapp",
  "adapter.telegram": "telegram",
  "adapter.github": "github",
  "adapter.slack": "slack",
  "adapter.discord": "discord",
  "adapter.notion": "notion",
  "adapter.linear": "linear",
  "adapter.jira": "jira",
  "adapter.airtable": "airtable",
  "adapter.webhook": "webhook",
  "adapter.database": "database"
};

export function ProviderIcon({ provider, size = 18, className, title }: ProviderIconProps) {
  const key = providerIconKey(provider);
  const Icon = providerIcons[key] ?? Cable;
  const label = title ?? providerIconLabel(key);

  return (
    <span
      aria-label={label}
      className={["provider-icon", `provider-icon-${key}`, className].filter(Boolean).join(" ")}
      role="img"
      title={label}
    >
      <Icon aria-hidden="true" size={size} />
    </span>
  );
}

export function providerIconKeyForAdapter(adapterId: string | undefined): string | undefined {
  return adapterId ? adapterProviderIcons[adapterId] : undefined;
}

export function providerIconKeyForAdapterIds(
  adapterIds: readonly string[] | undefined
): string | undefined {
  return adapterIds?.map(providerIconKeyForAdapter).find(Boolean);
}

function providerIconKey(provider: string | undefined): string {
  if (!provider) {
    return "connector";
  }

  return provider.toLowerCase().replace(/^adapter\./u, "");
}

function providerIconLabel(provider: string): string {
  switch (provider) {
    case "airtable":
      return "Airtable";
    case "discord":
      return "Discord";
    case "github":
      return "GitHub";
    case "gmail":
      return "Gmail";
    case "google":
      return "Google";
    case "jira":
      return "Jira";
    case "linear":
      return "Linear";
    case "notion":
      return "Notion";
    case "sheets":
      return "Google Sheets";
    case "slack":
      return "Slack";
    case "smtp":
      return "SMTP";
    case "telegram":
      return "Telegram";
    case "whatsapp":
      return "WhatsApp";
    case "mcp":
      return "MCP";
    case "openapi":
      return "OpenAPI";
    default:
      return provider.charAt(0).toUpperCase() + provider.slice(1);
  }
}

const providerIcons: Readonly<Record<string, ComponentType<SvgIconProps>>> = {
  google: GoogleIcon,
  gmail: GmailIcon,
  sheets: SheetsIcon,
  smtp: lucideIcon(Mail),
  email: lucideIcon(Mail),
  whatsapp: WhatsAppIcon,
  telegram: TelegramIcon,
  github: GitHubIcon,
  slack: SlackIcon,
  discord: DiscordIcon,
  notion: NotionIcon,
  linear: LinearIcon,
  jira: JiraIcon,
  airtable: AirtableIcon,
  webhook: lucideIcon(Webhook),
  database: lucideIcon(Database),
  openapi: lucideIcon(Table2),
  mcp: lucideIcon(Cable),
  connector: lucideIcon(Cable)
};

function lucideIcon(Icon: LucideIcon): ComponentType<SvgIconProps> {
  return function WrappedLucideIcon({ size = 18, ...rest }: SvgIconProps) {
    return <Icon {...rest} size={size} />;
  };
}

function svgProps(props: SvgIconProps) {
  const { size = 18, ...rest } = props;
  return {
    width: size,
    height: size,
    viewBox: "0 0 24 24",
    ...rest
  };
}

function GoogleIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path
        fill="#4285f4"
        d="M21.6 12.24c0-.68-.06-1.34-.18-1.96H12v3.7h5.38a4.6 4.6 0 0 1-2 3.02v2.5h3.22c1.88-1.73 3-4.28 3-7.26Z"
      />
      <path
        fill="#34a853"
        d="M12 22c2.7 0 4.96-.9 6.6-2.5L15.38 17c-.9.6-2.04.96-3.38.96-2.6 0-4.8-1.76-5.58-4.12H3.1v2.58A9.96 9.96 0 0 0 12 22Z"
      />
      <path
        fill="#fbbc05"
        d="M6.42 13.84A6.02 6.02 0 0 1 6.1 12c0-.64.12-1.26.32-1.84V7.58H3.1A9.96 9.96 0 0 0 2 12c0 1.6.38 3.12 1.1 4.42l3.32-2.58Z"
      />
      <path
        fill="#ea4335"
        d="M12 6.04c1.46 0 2.78.5 3.82 1.5l2.86-2.86C16.96 3.08 14.7 2.1 12 2.1a9.96 9.96 0 0 0-8.9 5.48l3.32 2.58C7.2 7.8 9.4 6.04 12 6.04Z"
      />
    </svg>
  );
}

function GmailIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path fill="#f2f4f8" d="M3 6.8h18v11.4H3z" />
      <path fill="#ea4335" d="M3 6.8 12 14l9-7.2v2.8L12 16.8 3 9.6z" />
      <path fill="#34a853" d="M3 9.6v8.6h3V12z" />
      <path fill="#4285f4" d="M18 12v6.2h3V9.6z" />
      <path fill="#fbbc05" d="M6 12 3 9.6V6.8l3 2.4zM18 12l3-2.4V6.8l-3 2.4z" />
    </svg>
  );
}

function SheetsIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path fill="#0f9d58" d="M6 2.5h8.2L19 7.3v14.2H6z" />
      <path fill="#87d3a2" d="M14.2 2.5v4.8H19z" />
      <path fill="#f7fff9" d="M8.6 10h7.8v7H8.6z" />
      <path
        fill="#0f9d58"
        d="M10 11.3h2v1.5h-2zm3.1 0H15v1.5h-1.9zM10 14h2v1.5h-2zm3.1 0H15v1.5h-1.9z"
      />
    </svg>
  );
}

function WhatsAppIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path
        fill="#25d366"
        d="M12 2.6a9.2 9.2 0 0 0-7.88 13.96L3 21.4l4.98-1.1A9.2 9.2 0 1 0 12 2.6Z"
      />
      <path
        fill="#fff"
        d="M9.18 7.25c-.2-.45-.42-.46-.62-.47h-.52c-.18 0-.46.07-.7.34-.24.26-.92.9-.92 2.2 0 1.3.94 2.55 1.08 2.73.13.18 1.82 2.92 4.5 3.98 2.22.88 2.68.7 3.16.66.48-.05 1.55-.64 1.77-1.25.22-.62.22-1.15.15-1.25-.07-.11-.24-.18-.5-.31l-1.78-.88c-.26-.09-.44-.13-.63.13-.18.27-.72.88-.88 1.06-.16.17-.32.2-.58.06-.27-.13-1.13-.41-2.14-1.32-.79-.7-1.32-1.57-1.48-1.84-.15-.26-.02-.4.12-.53.12-.12.27-.31.4-.46.13-.16.18-.27.27-.45.09-.18.04-.34-.02-.47z"
      />
    </svg>
  );
}

function TelegramIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <circle cx="12" cy="12" r="9.4" fill="#26a5e4" />
      <path
        fill="#fff"
        d="M17.9 7.2 15.8 17c-.16.7-.58.88-1.17.55l-3.22-2.38-1.55 1.5c-.17.17-.31.31-.64.31l.23-3.28 5.98-5.4c.26-.23-.06-.36-.4-.13l-7.4 4.66-3.18-1c-.7-.22-.7-.7.15-1.02l12.44-4.8c.58-.2 1.08.14.88 1.2Z"
      />
    </svg>
  );
}

function GitHubIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path
        fill="currentColor"
        d="M12 .5a12 12 0 0 0-3.8 23.38c.6.11.82-.26.82-.58v-2.04c-3.34.73-4.04-1.6-4.04-1.6-.55-1.39-1.34-1.76-1.34-1.76-1.09-.75.08-.73.08-.73 1.2.08 1.84 1.24 1.84 1.24 1.08 1.84 2.82 1.3 3.5 1 .11-.78.42-1.31.76-1.61-2.66-.3-5.46-1.33-5.46-5.93 0-1.31.47-2.38 1.23-3.22-.12-.3-.54-1.53.12-3.18 0 0 1-.32 3.3 1.23A11.5 11.5 0 0 1 12 4.55c1.02.01 2.05.14 3.02.4 2.3-1.55 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.77.84 1.23 1.91 1.23 3.22 0 4.61-2.8 5.63-5.48 5.93.43.37.82 1.1.82 2.22v3.29c0 .32.22.7.83.58A12 12 0 0 0 12 .5Z"
      />
    </svg>
  );
}

function SlackIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path fill="#36c5f0" d="M8.3 3a2.1 2.1 0 0 0 0 4.2h2.1V5.1A2.1 2.1 0 0 0 8.3 3Z" />
      <path fill="#36c5f0" d="M4.1 8.3a2.1 2.1 0 0 0 2.1 2.1h2.1V8.3H6.2a2.1 2.1 0 0 0-2.1 0Z" />
      <path fill="#2eb67d" d="M21 8.3a2.1 2.1 0 0 0-4.2 0v2.1h2.1A2.1 2.1 0 0 0 21 8.3Z" />
      <path fill="#2eb67d" d="M15.7 4.1a2.1 2.1 0 0 0-2.1 2.1v2.1h2.1V6.2a2.1 2.1 0 0 0 0-2.1Z" />
      <path fill="#ecb22e" d="M15.7 21a2.1 2.1 0 0 0 0-4.2h-2.1v2.1a2.1 2.1 0 0 0 2.1 2.1Z" />
      <path fill="#ecb22e" d="M19.9 15.7a2.1 2.1 0 0 0-2.1-2.1h-2.1v2.1h2.1a2.1 2.1 0 0 0 2.1 0Z" />
      <path fill="#e01e5a" d="M3 15.7a2.1 2.1 0 0 0 4.2 0v-2.1H5.1A2.1 2.1 0 0 0 3 15.7Z" />
      <path fill="#e01e5a" d="M8.3 19.9a2.1 2.1 0 0 0 2.1-2.1v-2.1H8.3v2.1a2.1 2.1 0 0 0 0 2.1Z" />
    </svg>
  );
}

function DiscordIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path
        fill="#5865f2"
        d="M19.3 5.2A16 16 0 0 0 15.3 4l-.48.98a14.7 14.7 0 0 0-5.64 0L8.7 4a16 16 0 0 0-4 1.2C2.2 9 1.55 12.72 1.9 16.4a16.3 16.3 0 0 0 5 2.52l1.04-1.7a10.6 10.6 0 0 1-1.64-.78l.4-.3c3.18 1.48 6.62 1.48 9.76 0l.4.3c-.52.31-1.06.57-1.64.78l1.04 1.7a16.3 16.3 0 0 0 5-2.52c.42-4.25-.7-7.94-1.96-11.2Z"
      />
      <circle cx="9.2" cy="12.4" r="1.25" fill="#fff" />
      <circle cx="14.8" cy="12.4" r="1.25" fill="#fff" />
    </svg>
  );
}

function NotionIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <rect x="3.2" y="3.2" width="17.6" height="17.6" rx="2.2" fill="#fff" />
      <path fill="#111" d="M7 7.2h3l4.4 6.8V7.2H17v9.6h-2.8L9.6 9.7v7.1H7z" />
    </svg>
  );
}

function LinearIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <circle cx="12" cy="12" r="9.4" fill="#5e6ad2" />
      <path
        stroke="#fff"
        strokeLinecap="round"
        strokeWidth="2"
        d="M7.2 15.8 15.8 7.2M5.7 12.6l5.7-5.7M12.6 18.3l5.7-5.7"
      />
    </svg>
  );
}

function JiraIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path fill="#2684ff" d="M12 3 21 12l-9 9-3.2-3.2 5.8-5.8-5.8-5.8z" />
      <path fill="#0052cc" d="M12 3 3 12l9 9 3.2-3.2L9.4 12l5.8-5.8z" opacity=".9" />
    </svg>
  );
}

function AirtableIcon(props: SvgIconProps) {
  return (
    <svg {...svgProps(props)}>
      <path fill="#ffbf00" d="M11.4 3.2 3.3 6.6l8.1 3.3 8.2-3.3z" />
      <path fill="#18bfff" d="M12.6 10.4v10.4l8.1-3.35V7.05z" />
      <path fill="#f82b60" d="M11.4 10.4 3.3 7.05v10.4l8.1 3.35z" />
    </svg>
  );
}
