import { toFile, Uploadable } from "cloudflare";
import { CFAccount } from "./api";
import { randCode, randString } from "./random";

interface Script {
    script: Uploadable;
    path: string;
}

export async function buildScript(
    account: CFAccount,
    workerName: string,
    subdomain: string,
    filename: string,
    preRelease: boolean
): Promise<Script> {
    const pathCharset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456780-_';
    const passCharset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@$&*_-+;:,.';

    let url = 'https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/latest/download/worker.js';
    if (preRelease) {
        const res = await fetch('https://raw.githubusercontent.com/bia-pain-bache/BPB-Worker-Panel/refs/heads/dev/package.json');
        if(!res.ok) {
            throw new Error(`Failed to get pre-release script: status ${res.status} at ${res.url}`);
        }

        const { version } = await res.json() as any;
        url = `https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/download/v${version}/worker.js`;
    }

    const res = await fetch(url);
    if (!res.ok) {
        throw new Error(`Failed to get panel script: status ${res.status} at ${res.url}`)
    }

    const script = await res.text();
    const buildTimestamp = new Date().toISOString();
    const randomCode = randCode();
    const path = randString(pathCharset, 12, 16);

    const embededSettings = {
        accID: account.id,
        accEmail: account.email,
        apiToken: account.token,
        vlUUID: crypto.randomUUID(),
        trPass: randString(passCharset, 16, 20),
        securePath: path,
        proxyIpMode: 'proxyip',
        proxyIPs: [],
        prefixes: [],
        fallback: '',
        dohUrl: '',
        mainDomain: `${workerName}.${subdomain}`
    };

    const worker = [
        `// ${account.email}`,
        `// Build: ${buildTimestamp}`,
        '// @ts-nocheck',
        `${randomCode}const EMBEDED_SETTINGS = ${JSON.stringify(embededSettings)};${script}`
    ].join('\n');

    const file = new TextEncoder().encode(worker);
    const uploadable: Uploadable = await toFile(file, filename, {
        type: 'application/javascript+module',
    });

    return {
        script: uploadable,
        path: path
    }
}
