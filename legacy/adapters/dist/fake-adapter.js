export class FakeAdapter {
    metadata;
    invocations = [];
    constructor(metadata) {
        this.metadata = metadata;
    }
    async invoke(invocation) {
        if (invocation.adapterId !== this.metadata.id) {
            throw new Error(`Invocation targeted adapter '${invocation.adapterId}' but fake adapter is '${this.metadata.id}'.`);
        }
        const recorded = {
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
export function createFakeAdapter(metadata) {
    return new FakeAdapter(metadata);
}
//# sourceMappingURL=fake-adapter.js.map