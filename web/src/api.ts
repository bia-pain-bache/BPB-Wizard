import Cloudflare, { Uploadable } from 'cloudflare';
import { randSubdomain } from './random';

export class CFAccount {
    readonly token: string;
    readonly id: string;
    readonly email: string;
    readonly client: Cloudflare;

    private constructor(token: string, client: Cloudflare, id: string, email: string) {
        this.token = token;
        this.client = client;
        this.id = id;
        this.email = email;
    }

    static async create(token: string): Promise<CFAccount> {
        const client = new Cloudflare({ apiToken: token });

        const response = await client.user.tokens.verify();
        if (response.status !== 'active') {
            throw new Error(`API token is ${response.status}.`)
        }

        const [accounts, user] = await Promise.all([
            client.accounts.list(),
            client.user.get(),
        ]);

        return new CFAccount(token, client, accounts.result[0].id, user.email.toLowerCase());
    }

    async nameTaken(deployType: string, name: string): Promise<boolean> {
        try {
            if (deployType === "pages") {
                await this.client.pages.projects.get(name, {
                    account_id: this.id,
                });
            }

            await this.client.workers.scripts.get(name, {
                account_id: this.id,
            });

            return true;
        } catch (error) {
            return false;
        }
    }

    async createKvNamespace(workerName: string, deployType: string): Promise<string> {
        const now = new Date();
        const title = `${workerName}-${deployType}-${now.toISOString()}`;

        const namespace = await this.client.kv.namespaces.create({
            account_id: this.id,
            title,
        });

        return namespace.id;
    }

    async getWorkersDevSubdomain(): Promise<string> {
        const res = await this.client.workers.subdomains.get({
            account_id: this.id,
        });

        return `${res.subdomain}.workers.dev`;
    }

    async createWorkersDevSubdomain(): Promise<string> {
        const maxAttempts = 3;

        for (let i = 0; i < maxAttempts; i++) {
            try {
                const res = await this.client.workers.subdomains.update({
                    account_id: this.id,
                    subdomain: randSubdomain(),
                });

                return res.subdomain;
            } catch (err) {
                continue;
            }
        }

        throw new Error(`Failed to create a unique workers.dev subdomain after ${maxAttempts} attempts.`);
    }

    async deployWorker(name: string, script: Uploadable, namespaceId: string) {
        // TS SDK has bugs for workers deployment - Uploading files
        const date = new Date().toISOString().split('T')[0];
        const metadata = {
            main_module: 'worker.js',
            compatibility_date: date,
            compatibility_flags: ['nodejs_compat'],
            bindings: [
                { type: 'kv_namespace', name: 'kv', namespace_id: namespaceId }
            ]
        };

        const uploadForm = new FormData();
        uploadForm.append('metadata', new Blob([JSON.stringify(metadata)], { type: 'application/json' }));
        uploadForm.append('worker.js', script as File, 'worker.js');

        const res = await fetch(`https://api.cloudflare.com/client/v4/accounts/${this.id}/workers/scripts/${name}`, {
            method: 'PUT',
            headers: { 'Authorization': `Bearer ${this.token}` },
            body: uploadForm
        });

        const data = await res.json() as any;
        if (!res.ok || !data.success) {
            throw new Error(`Error deploying worker: ${JSON.stringify(data.errors, null, 2)}`)
        }

        // await this.client.workers.scripts.update(name, {
        //     account_id: this.id,
        //     metadata: {
        //         main_module: 'worker.js',
        //         compatibility_date: date,
        //         compatibility_flags: ['nodejs_compat'],
        //         bindings: [{
        //             name: 'kv',
        //             namespace_id: namespaceId,
        //             type: 'kv_namespace',
        //         }],
        //     },
        //     files: [uploadable]
        // });
    }

    async enableSubdomain(name: string) {
        await this.client.workers.scripts.subdomain.create(name, {
            account_id: this.id,
            enabled: true,
            previews_enabled: true,
        });
    }

    async createPagesProject(name: string, namespaceId: string): Promise<string> {
        const date = new Date().toISOString().split('T')[0];

        const project = await this.client.pages.projects.create({
            account_id: this.id,
            name: name,
            production_branch: 'main',
            deployment_configs: {
                production: {
                    browsers: {},
                    compatibility_date: date,
                    compatibility_flags: ['nodejs_compat'],
                    kv_namespaces: {
                        'kv': { namespace_id: namespaceId }
                    }
                }
            }
        });

        return project.subdomain ?? '';
    }

    async deployPages(name: string, script: Uploadable) {
        await this.client.pages.projects.deployments.create(name, {
            account_id: this.id,
            branch: 'main',
            manifest: '{}',
            "_worker.js": script
        });
    }
}


