export type Logger = (message: string) => void;

export interface StreamLogger {
    readable: ReadableStream;
    success: Logger;
    info: Logger;
    error: Logger;
    complete: Logger;
    close: () => Promise<void>;
}

export function createStreamLogger(): StreamLogger {
    const { readable, writable } = new TransformStream();
    const writer = writable.getWriter();
    const encoder = new TextEncoder();

    const send = (type: string, message: string) =>
        writer.write(encoder.encode(JSON.stringify({ type, message }) + '\n'));

    return {
        readable,
        success: (message) => send('success', message),
        info: (message) => send('info', message),
        error: (message) => send('error', message),
        complete: (message) => send('complete', message),
        close: () => writer.close()
    };
}