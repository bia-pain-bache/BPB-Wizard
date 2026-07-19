const encoder = new TextEncoder();
const decoder = new TextDecoder();

async function importKey(secret: string): Promise<CryptoKey> {
    const hash = await crypto.subtle.digest(
        'SHA-256',
        encoder.encode(secret)
    );

    return crypto.subtle.importKey(
        'raw',
        hash,
        'AES-GCM',
        false,
        ['encrypt', 'decrypt']
    );
}

export async function encrypt(text: string, secret: string): Promise<string> {
    const key = await importKey(secret);
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const encrypted = await crypto.subtle.encrypt(
        { name: 'AES-GCM', iv },
        key,
        encoder.encode(text)
    );

    const result = new Uint8Array(iv.length + encrypted.byteLength);
    result.set(iv);
    result.set(new Uint8Array(encrypted), iv.length);
    const base64Result = btoa(String.fromCharCode(...result));

    return base64Result;
}

export async function decrypt(encrypted: string, secret: string): Promise<string> {
    try {
        const key = await importKey(secret);
        const bytes = Uint8Array.from(atob(encrypted), c => c.charCodeAt(0));
        const iv = bytes.slice(0, 12);
        const data = bytes.slice(12);
        const decrypted = await crypto.subtle.decrypt(
            { name: 'AES-GCM', iv },
            key,
            data
        );

        return decoder.decode(decrypted);
    } catch (error) {
        throw new Error('Wizard API token had been changed since v5.1.0, Please go to wizard main page and create a new token.\n');
    }
}