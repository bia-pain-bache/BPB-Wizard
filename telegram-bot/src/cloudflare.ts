import Cloudflare from 'cloudflare';


export class CloudflareManager {
  private client: Cloudflare;
  public accountId: string = '';
  public email: string = '';

  constructor(private token: string) {
    this.client = new Cloudflare({ apiToken: token });
  }

  async verifyAndInitialize(): Promise<boolean> {
    try {
      const tokenRes = await this.client.user.tokens.verify();
      if (tokenRes.status !== 'active') return false;

      const accountsRes = await this.client.accounts.list();
      if (accountsRes.result.length === 0) return false;
      this.accountId = accountsRes.result[0].id;

      const userRes = (await this.client.user.get()) as unknown as { email: string };
      this.email = userRes.email;
      
      return true;
    } catch (error) {
      console.error('Failed to verify token:', error);
      return false;
    }
  }

  async isNameTaken(deployType: 'workers' | 'pages', name: string): Promise<boolean> {
    try {
      if (deployType === 'pages') {
        await this.client.pages.projects.get(name, { account_id: this.accountId });
        return true;
      } else {
        await this.client.workers.scripts.get(name, { account_id: this.accountId });
        return true;
      }
    } catch (e: any) {
      if (e.status === 404) return false;
      throw e;
    }
  }

  async createKVNamespace(workerName: string, deployType: string): Promise<string> {
    const title = `${workerName}-${deployType}-${new Date().getTime()}`;
    const namespace = await this.client.kv.namespaces.create({
      account_id: this.accountId,
      title: title
    });
    return namespace.id as string;
  }

  async getWorkersDevSubdomain(): Promise<string | null> {
    try {
      const res = await this.client.workers.subdomains.get({ account_id: this.accountId });
      return res.subdomain ? `${res.subdomain}.workers.dev` : null;
    } catch (e: any) {
      if (e.status === 404) return null;
      throw e;
    }
  }

  async createWorkersDevSubdomain(subdomain: string): Promise<string> {
    const res = await this.client.workers.subdomains.update({
      account_id: this.accountId,
      subdomain: subdomain
    });
    return `${res.subdomain}.workers.dev`;
  }

  async buildScript(workerName: string, subdomain: string): Promise<{ script: string; securePath: string }> {
    const rawRes = await fetch('https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/latest/download/worker.js');
    if (!rawRes.ok) throw new Error('Failed to fetch worker.js from repository');
    const rawWorkerJs = await rawRes.text();

    const generateRandomStr = (length: number, chars: string) => {
      let result = '';
      for (let i = 0; i < length; i++) {
        result += chars.charAt(Math.floor(Math.random() * chars.length));
      }
      return result;
    };

    const passCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@$&*_-+;:,.";
    const pathCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456780-_";

    const trPass = generateRandomStr(Math.floor(Math.random() * 16) + 16, passCharset);
    const securePath = generateRandomStr(Math.floor(Math.random() * 16) + 16, pathCharset);
    const vlUUID = crypto.randomUUID();

    const settings = {
      accID: this.accountId,
      accEmail: this.email,
      apiToken: this.token,
      vlUUID: vlUUID,
      trPass: trPass,
      securePath: securePath,
      proxyIpMode: "proxyip",
      proxyIPs: [],
      prefixes: [],
      fallback: "",
      dohUrl: "",
      mainDomain: `${workerName}.${subdomain}`
    };

    const buildTimestamp = new Date().toISOString();
    const randCode = `const _rand = "${generateRandomStr(8, 'abcdefghijklmnopqrstuvwxyz0123456789')}";`;

    const script = `// ${this.email}\n// ${buildTimestamp}\n// @ts-nocheck\n${randCode}\nconst EMBEDED_SETTINGS = ${JSON.stringify(settings)};\n${rawWorkerJs}`;

    return { script, securePath };
  }

  async deployWorker(name: string, script: string, namespaceID: string): Promise<void> {
    const formData = new FormData();
    const metadata = {
      main_module: 'worker.js',
      compatibility_date: new Date().toISOString().split('T')[0],
      compatibility_flags: ['nodejs_compat'],
      bindings: [
        { name: 'kv', namespace_id: namespaceID, type: 'kv_namespace' }
      ]
    };
    
    // Test with Blob for metadata
    formData.append('metadata', new Blob([JSON.stringify(metadata)], { type: 'application/json' }), 'metadata.json');
    // Test with File for worker
    const file = new File([script], 'worker.js', { type: 'application/javascript+module' });
    formData.append('worker.js', file);

    const res = await fetch(`https://api.cloudflare.com/client/v4/accounts/${this.accountId}/workers/scripts/${name}`, {
      method: 'PUT',
      headers: {
        'Authorization': `Bearer ${this.token}`
      },
      body: formData as any
    });

    if (!res.ok) {
      const errText = await res.text();
      throw new Error(`Failed to deploy worker: ${errText}`);
    }
  }

  async enableWorkerSubdomain(name: string): Promise<void> {
    await this.client.workers.scripts.subdomain.create(name, {
      account_id: this.accountId,
      enabled: true
    });
  }

  async createPagesProject(name: string, namespaceID: string): Promise<string> {
    const project = await this.client.pages.projects.create({
      account_id: this.accountId,
      name: name,
      production_branch: 'main',
      deployment_configs: {
        production: {
          compatibility_date: new Date().toISOString().split('T')[0],
          compatibility_flags: ['nodejs_compat'],
          kv_namespaces: {
            kv: { namespace_id: namespaceID }
          }
        }
      }
    });
    return project.subdomain as string;
  }

  async deployPagesScript(name: string, script: string): Promise<void> {
    const formData = new FormData();
    formData.append('_worker.js', new Blob([script], { type: 'application/javascript+module' }), '_worker.js');
    formData.append('branch', 'main');
    formData.append('manifest', '{}');

    const res = await fetch(`https://api.cloudflare.com/client/v4/accounts/${this.accountId}/pages/projects/${name}/deployments`, {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${this.token}`
      },
      body: formData as any
    });

    if (!res.ok) {
      const errText = await res.text();
      throw new Error(`Failed to deploy Pages script: ${errText}`);
    }
  }

  async setTelegramBotKV(namespaceId: string, botToken: string, telegramUserId: string): Promise<void> {
    const value = JSON.stringify({
      telegramBotToken: botToken,
      telegramUserId: telegramUserId
    });

    const res = await fetch(`https://api.cloudflare.com/client/v4/accounts/${this.accountId}/storage/kv/namespaces/${namespaceId}/values/telegramBot`, {
      method: 'PUT',
      headers: {
        'Authorization': `Bearer ${this.token}`,
      },
      body: value
    });

    if (!res.ok) {
      const errText = await res.text();
      throw new Error(`Failed to save Telegram Bot settings to KV: ${errText}`);
    }
  }

  async listDeployments(deployType: 'workers' | 'pages'): Promise<any[]> {
    if (deployType === 'workers') {
      const res = await this.client.workers.scripts.list({ account_id: this.accountId });
      return res.result || [];
    } else {
      const res = await this.client.pages.projects.list({ account_id: this.accountId });
      return res.result || [];
    }
  }

  async deleteDeployment(deployType: 'workers' | 'pages', name: string): Promise<void> {
    if (deployType === 'workers') {
      await this.client.workers.scripts.delete(name, { account_id: this.accountId });
    } else {
      await this.client.pages.projects.delete(name, { account_id: this.accountId });
    }
  }
}
