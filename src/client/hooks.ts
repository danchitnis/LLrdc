type Listener<T> = (payload: T) => void;

export class ClientEventEmitter<TEvents extends object> {
    private listeners = new Map<keyof TEvents, Set<Listener<TEvents[keyof TEvents]>>>();

    public on<TKey extends keyof TEvents>(event: TKey, listener: Listener<TEvents[TKey]>): () => void {
        const current = this.listeners.get(event) ?? new Set();
        current.add(listener as Listener<TEvents[keyof TEvents]>);
        this.listeners.set(event, current);
        return () => {
            current.delete(listener as Listener<TEvents[keyof TEvents]>);
            if (current.size === 0) {
                this.listeners.delete(event);
            }
        };
    }

    public emit<TKey extends keyof TEvents>(event: TKey, payload: TEvents[TKey]) {
        const current = this.listeners.get(event);
        if (!current) {
            return;
        }
        for (const listener of current) {
            listener(payload as TEvents[keyof TEvents]);
        }
    }
}
