export default {
	async fetch(request, env) {
		const url = new URL(request.url);

		if (url.pathname === '/api/deploy' && request.method === 'POST') {
			const origin = request.headers.get('Origin').toLowerCase();
			if (origin !== url.origin) {
				return new Response('Unauthorized context', { status: 403 });
			} 

			const { readable, writable } = new TransformStream();
			const writer = writable.getWriter();
			const encoder = new TextEncoder();

			const send = (type, message) => writer.write(encoder.encode(JSON.stringify({ type, message }) + '\n'));
			const log = (message) => send('log', message);
			const error = (message) => send('error', message);
			const complete = (message) => send('complete', message);

			(async () => {
				try {
					const key = url.searchParams?.get('key');
					const formData = await request.formData();
					const apiToken = key ? await decrypt(key, env.SECRET) : formData.get('apiToken')?.trim();
					const deployType = formData.get('deployType')?.trim() || 'workers';

					const { accountId, accountName } = await validateToken(apiToken, log, error);
					if (!accountId) return;

					const workerName = generateRandomString('abcdefghijklmnopqrstuvwxyz0123456789');
					const validSubdomain = await validateSubdomain(workerName, deployType, accountId, apiToken, log, error);
					if (!validSubdomain) return;

					const namespaceId = await createKv(accountId, apiToken, workerName, deployType, log, error);
					if (!namespaceId) return;

					if (deployType === 'pages') {
						await deployPages(env, accountId, accountName, apiToken, workerName, namespaceId, log, error, complete);
					} else {
						await deployWorkers(env, accountId, accountName, apiToken, workerName, namespaceId, log, error, complete);
					}
				} catch (err) {
					await error('Execution exception crash: ' + err.message);
				} finally {
					await writer.close();
				}
			})();

			return new Response(readable, {
				headers: {
					'Content-Type': 'application/x-ndjson'
				}
			});
		}

		if (url.pathname === '/') {
			return env.ASSETS.fetch(new URL('/index.html', request.url));
		}

		return env.ASSETS.fetch(request);
	}
};

const createCfApi = (accountId) => {
	const base = 'https://api.cloudflare.com/client/v4';
	if (!accountId) return {
		accounts: `${base}/accounts`,
		tokens: `${base}/user/tokens/verify`,
	}

	return {
		kv: `${base}/accounts/${accountId}/storage/kv/namespaces`,
		workers: `${base}/accounts/${accountId}/workers`,
		workersScripts: `${base}/accounts/${accountId}/workers/scripts`,
		pages: `${base}/accounts/${accountId}/pages`,
		pagesProjects: `${base}/accounts/${accountId}/pages/projects`
	};
};

async function validateToken(apiToken, log, error) {
	await log(`Checking API Token...`);
	const api = createCfApi();
	const res = await fetch(api.tokens, {
		method: 'GET',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' }
	});
	const data = await res.json();
	if (!data.success) {
		await error('API token is not valid.');
		return false;
	}

	const listRes = await fetch(api.accounts, {
		method: 'GET',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' }
	});
	const listData = await listRes.json();
	if (!listData.success) {
		await error('Credentials are not valid, check Account ID and API token again.');
		return false;
	}

	return {
		accountId: listData.result[0].id,
		accountName: listData.result[0].name
	}
}

async function validateSubdomain(workerName, deployType, accountId, apiToken, log, error) {
	const api = createCfApi(accountId);
	const url = deployType === 'pages' ? api.pagesProjects : api.workersScripts;
	const res = await fetch(`${url}/${workerName}`, {
		method: 'GET',
		headers: { 'Authorization': `Bearer ${apiToken}` }
	});

	if (res.status === 200) {
		await error(`The name '${workerName}' is already taken.`);
		return false;
	}

	return true;
}

async function createKv(accountId, apiToken, workerName, deployType, log, error) {
	await log(`Creating KV namespace...`);
	const now = new Date();
	const kvName = `${workerName}-${deployType}-${now.toISOString()}`;
	const api = createCfApi(accountId);
	const res = await fetch(api.kv, {
		method: 'POST',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' },
		body: JSON.stringify({ title: kvName })
	});
	const data = await res.json();
	if (!data.success) {
		await error('Failed to create KV storage.');
		return;
	}

	return data.result.id;
}

async function buildScript(accountId, accountName, apiToken, workerName, subdomain, error) {
	const url = 'https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/download/v5.0.0/worker.js';
	const charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456780-_';

	const res = await fetch(url);
	if (!res.ok) {
		await error('Failed to get panel script.');
		return;
	}

	let script = await res.text();
	const embededSettings = {
		accID: accountId,
		accEmail: extractEmail(accountName),
		apiToken: apiToken,
		vlUUID: crypto.randomUUID(),
		trPass: generateRandomString(charset, 16),
		securePath: generateRandomString(charset, 16),
		proxyIpMode: 'proxyip',
		proxyIPs: [],
		prefixes: [],
		fallback: '',
		dohUrl: '',
		mainDomain: `${workerName}.${subdomain}`
	};

	const worker = `
        const EMBEDED_SETTINGS = ${JSON.stringify(embededSettings)};
        ${script}`;

	return {
		script: worker,
		settings: embededSettings
	}
}

async function deployPages(env, accountId, accountName, apiToken, workerName, namespaceId, log, error, complete) {
	await log('Building BPB Panel script...');
	const { script, settings } = await buildScript(accountId, accountName, apiToken, workerName, 'pages.dev', error);

	await log(`Creating Pages project...`);
	const api = createCfApi(accountId);
	const createRes = await fetch(api.pagesProjects, {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${apiToken}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({
			name: workerName,
			production_branch: 'main',
			deployment_configs: {
				production: {
					compatibility_flags: ['nodejs_compat'],
					kv_namespaces: {
						kv: { namespace_id: namespaceId }
					}
				}
			}
		})
	});

	const createData = await createRes.json();
	if (!createData.success) {
		await error('Pages project creation failed.');
		return;
	}

	await log('Deploying BPB Panel...');
	const uploadForm = new FormData();
	uploadForm.append('manifest', '{}');
	uploadForm.append('_worker.js', new Blob([script], { type: 'application/javascript' }), '_worker.js');
	const deployRes = await fetch(`${api.pagesProjects}/${workerName}/deployments`, {
		method: 'POST',
		headers: { 'Authorization': `Bearer ${apiToken}` },
		body: uploadForm
	});

	const deployData = await deployRes.json();
	if (!deployData.success) {
		await error('Pages Deployment Refused: ' + JSON.stringify(deployData.errors));
		return;
	}

	const path = encodeURIComponent(settings.securePath);
	const payload = {
		url: `https://${createData.result.subdomain}/${path}/panel`,
		user: accountName,
		key: await encrypt(apiToken, env.SECRET)
	}

	await complete(JSON.stringify(payload));
}

async function deployWorkers(env, accountId, accountName, apiToken, workerName, namespaceId, log, error, complete) {
	const api = createCfApi(accountId);

	const subRes = await fetch(`${api.workers}/subdomain`, {
		headers: { 'Authorization': `Bearer ${apiToken}` }
	});
	const subData = await subRes.json();
	const subdomain = `${subData.result.subdomain}.workers.dev`;

	await log('Building BPB Panel script...');
	const { script, settings } = await buildScript(accountId, accountName, apiToken, workerName, subdomain, error);

	await log('Deploying BPB Panel...');
	const metadata = {
		main_module: 'worker.js',
		compatibility_date: new Date().toISOString().split('T')[0],
		compatibility_flags: ['nodejs_compat'],
		bindings: [
			{ type: 'kv_namespace', name: 'kv', namespace_id: namespaceId }
		]
	};

	const uploadForm = new FormData();
	uploadForm.append('metadata', new Blob([JSON.stringify(metadata)], { type: 'application/json' }));
	uploadForm.append('worker.js', new Blob([script], { type: 'application/javascript+module' }), 'worker.js');

	const deployRes = await fetch(`${api.workersScripts}/${workerName}`, {
		method: 'PUT',
		headers: { 'Authorization': `Bearer ${apiToken}` },
		body: uploadForm
	});

	const deployData = await deployRes.json();
	if (!deployData.success) {
		console.log('Deploy Data:', JSON.stringify(deployData, null, 2));
		await error('Deployment Failed.');
		return;
	}

	await log('Activating subdomain...');
	await fetch(`${api.workersScripts}/${workerName}/subdomain`, {
		method: 'POST',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' },
		body: JSON.stringify({ enabled: true, previews_enabled: true })
	});

	const path = encodeURIComponent(settings.securePath);
	const payload = {
		url: `https://${workerName}.${subdomain}/${path}/panel`,
		user: accountName,
		key: await encrypt(apiToken, env.SECRET)
	}

	await complete(JSON.stringify(payload));
}

function generateRandomString(charset, len) {
	const length = len ?? Math.floor(Math.random() * (32 - 16 + 1)) + 16;
	const arr = new Uint8Array(length);
	crypto.getRandomValues(arr);
	let string = '';
	for (let i = 0; i < length; i++) { string += charset[arr[i] % charset.length]; }
	return string;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

async function importKey(secret) {
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

async function encrypt(text, secret) {
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

	return encodeURIComponent(base64Result);
}

async function decrypt(encrypted, secret) {
	const key = await importKey(secret);
	const decoded = decodeURIComponent(encrypted);
	const bytes = Uint8Array.from(atob(decoded), c => c.charCodeAt(0));
	const iv = bytes.slice(0, 12);
	const data = bytes.slice(12);
	const decrypted = await crypto.subtle.decrypt(
		{ name: 'AES-GCM', iv },
		key,
		data
	);

	return decoder.decode(decrypted);
}

function extractEmail(str) {
	// const match = str.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
	// return match ? match[0] : null;
	return str.split("'s ")[0].toLowerCase();
}