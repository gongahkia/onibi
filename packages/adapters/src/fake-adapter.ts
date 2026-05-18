import type {
  Adapter,
  AdapterInvocation,
  AdapterMetadata,
  AdapterResult,
  RecordedAdapterInvocation
} from "./types.js";

export class FakeAdapter implements Adapter {
  readonly metadata: AdapterMetadata;
  readonly invocations: RecordedAdapterInvocation[] = [];

  public constructor(metadata: AdapterMetadata) {
    this.metadata = metadata;
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    if (invocation.adapterId !== this.metadata.id) {
      throw new Error(
        `Invocation targeted adapter '${invocation.adapterId}' but fake adapter is '${this.metadata.id}'.`
      );
    }

    const recorded: RecordedAdapterInvocation = {
      ...invocation,
      sequence: this.invocations.length + 1
    };
    this.invocations.push(recorded);

    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      status: "recorded",
      receipt: {
        fake: true,
        sequence: recorded.sequence,
        idempotencyKey: invocation.idempotencyKey ?? null
      }
    };
  }
}

export function createFakeAdapter(metadata: AdapterMetadata): FakeAdapter {
  return new FakeAdapter(metadata);
}
