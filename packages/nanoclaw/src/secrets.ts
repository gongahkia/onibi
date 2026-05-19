import { redactSecretString } from "@kelpclaw/workflow-spec";

export interface SecretResolutionContext {
  readonly workflowId: string;
  readonly revision: number;
  readonly nodeId: string;
  readonly runId: string;
  readonly secretName: string;
}

export interface SecretResolver {
  resolve(secretRef: string, context: SecretResolutionContext): Promise<string>;
}

export interface SecretValueStore {
  getSecretValue(name: string): Promise<string | null>;
}

export class SecretStoreResolver implements SecretResolver {
  public constructor(private readonly store: SecretValueStore) {}

  public async resolve(secretRef: string): Promise<string> {
    if (!secretRef.startsWith("secret:")) {
      return new EnvironmentSecretResolver().resolve(secretRef);
    }

    const name = secretRef.slice("secret:".length);
    const value = await this.store.getSecretValue(name);
    if (value) {
      return value;
    }

    return new EnvironmentSecretResolver().resolve(secretRef);
  }
}

export class EnvironmentSecretResolver implements SecretResolver {
  public async resolve(secretRef: string): Promise<string> {
    if (secretRef.startsWith("mock:")) {
      return secretRef;
    }

    if (secretRef.startsWith("env:")) {
      const envName = secretRef.slice("env:".length);
      const value = process.env[envName];
      if (!value) {
        throw new Error(`Secret reference '${redactSecretString(secretRef)}' is not available.`);
      }

      return value;
    }

    if (secretRef.startsWith("secret:")) {
      const envName = secretEnvironmentName(secretRef.slice("secret:".length));
      const value = process.env[envName];
      if (!value) {
        throw new Error(`Secret reference '${redactSecretString(secretRef)}' is not available.`);
      }

      return value;
    }

    throw new Error(`Secret reference '${redactSecretString(secretRef)}' cannot be resolved.`);
  }
}

export function secretEnvironmentName(secretName: string): string {
  const suffix = secretName
    .replace(/[^a-zA-Z0-9]+/gu, "_")
    .replace(/^_+|_+$/gu, "")
    .toUpperCase();

  return `KELPCLAW_SECRET_${suffix || "VALUE"}`;
}
