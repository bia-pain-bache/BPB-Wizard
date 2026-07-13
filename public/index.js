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

					const { accountId, userEmail } = await validateToken(apiToken, log, error);
					if (!accountId) return;

					const workerName = generateSubdomain();
					const validSubdomain = await validateSubdomain(workerName, deployType, accountId, apiToken, log, error);
					if (!validSubdomain) return;

					const namespaceId = await createKv(accountId, apiToken, workerName, deployType, log, error);
					if (!namespaceId) return;

					if (deployType === 'pages') {
						await deployPages(env, accountId, userEmail, apiToken, workerName, namespaceId, log, error, complete);
					} else {
						await deployWorkers(env, accountId, userEmail, apiToken, workerName, namespaceId, log, error, complete);
					}
				} catch (err) {
					await error('Execution crash: ' + err.message);
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
	const accounts = `${base}/accounts`;
	
	if (!accountId) return {
		accounts,
		user: `${base}/user`,
		tokens: `${base}/user/tokens/verify`,
	}

	const userAccount = `${accounts}/${accountId}`;

	return {
		kv: `${userAccount}/storage/kv/namespaces`,
		workers: `${userAccount}/workers`,
		workersScripts: `${userAccount}/workers/scripts`,
		pages: `${userAccount}/pages`,
		pagesProjects: `${userAccount}/pages/projects`
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

	const accRes = await fetch(api.accounts, {
		method: 'GET',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' }
	});
	
	const accData = await accRes.json();
	if (!accData.success) {
		await error('Credentials are not valid, check API token or its permissions.');
		return false;
	}

	const userRes = await fetch(api.user, {
		method: 'GET',
		headers: { 'Authorization': `Bearer ${apiToken}`, 'Content-Type': 'application/json' }
	});
	
	const userData = await userRes.json();
	if (!userData.success) {
		await error('Credentials are not valid, check API token or its permissions');
		return false;
	}

	return {
		accountId: accData.result[0].id,
		userEmail: userData.result.email
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

async function buildScript(accountId, userEmail, apiToken, workerName, subdomain, error) {
	const url = 'https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/download/v5.1.0/worker.js';
	const pathCharset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456780-_';
	const passCharset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@$&*_-+;:,.';

	const res = await fetch(url);
	if (!res.ok) {
		await error('Failed to get panel script.');
		return;
	}

	const script = await res.text();
	const buildTimestamp = new Date().toISOString();
	const padding = paddCode();
	
	const embededSettings = {
		accID: accountId,
		accEmail: userEmail.toLowerCase(),
		apiToken: apiToken,
		vlUUID: crypto.randomUUID(),
		trPass: generateRandomString(passCharset, 16),
		securePath: generateRandomString(pathCharset, 16),
		proxyIpMode: 'proxyip',
		proxyIPs: [],
		prefixes: [],
		fallback: '',
		dohUrl: '',
		mainDomain: `${workerName}.${subdomain}`
	};

	const worker = [
		`// ${userEmail.toLowerCase()}`,
		`// Build: ${buildTimestamp}`,
		'// @ts-nocheck',
		`${padding}const EMBEDED_SETTINGS = ${JSON.stringify(embededSettings)};${script}`
	].join('\r\n');

	return {
		script: worker,
		settings: embededSettings
	}
}

async function deployPages(env, accountId, userEmail, apiToken, workerName, namespaceId, log, error, complete) {
	await log('Building BPB Panel script...');
	const { script, settings } = await buildScript(accountId, userEmail, apiToken, workerName, 'pages.dev', error);

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

	const url = new URL(`https://${createData.result.subdomain}/${settings.securePath}/panel`);
	const payload = {
		url: url.href,
		user: userEmail,
		key: await encrypt(apiToken, env.SECRET)
	}

	await complete(JSON.stringify(payload));
}

async function deployWorkers(env, accountId, userEmail, apiToken, workerName, namespaceId, log, error, complete) {
	const api = createCfApi(accountId);

	const subRes = await fetch(`${api.workers}/subdomain`, {
		headers: { 'Authorization': `Bearer ${apiToken}` }
	});
	
	const subData = await subRes.json();
	if (!subData.success) {
		await error('Failed to get global workers subdomain: ' + JSON.stringify(subData.errors));
		return;
	}
	const subdomain = `${subData.result.subdomain}.workers.dev`;

	await log('Building BPB Panel script...');
	const { script, settings } = await buildScript(accountId, userEmail, apiToken, workerName, subdomain, error);

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
		await error('Deployment Failed: ' + JSON.stringify(deployData.errors));
		return;
	}

	await log('Activating subdomain...');
	await fetch(`${api.workersScripts}/${workerName}/subdomain`, {
		method: 'POST',
		headers: { 
			'Authorization': `Bearer ${apiToken}`, 
			'Content-Type': 'application/json' 
		},
		body: JSON.stringify({ 
			enabled: true, 
			previews_enabled: true 
		})
	});

	const url = new URL(`https://${workerName}.${subdomain}/${settings.securePath}/panel`);
	const payload = {
		url: url.href,
		user: userEmail,
		key: await encrypt(apiToken, env.SECRET)
	}

	await complete(JSON.stringify(payload));
}

function generateSubdomain() {
	const charset = 'abcdefghijklmnopqrstuvwxyz0123456789--';
	let subdomain;
	do {
		subdomain = generateRandomString(charset);
	} while (subdomain.startsWith('-') || subdomain.endsWith('-'));

	return subdomain;
}

function generateRandomString(charset, len) {
	const length = len ?? Math.floor(Math.random() * (32 - 16 + 1)) + 16;
	const arr = new Uint8Array(length);
	crypto.getRandomValues(arr);
	let string = '';
	
	for (let i = 0; i < length; i++) { 
		string += charset[arr[i] % charset.length]; 
	}

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

	return base64Result;
}

async function decrypt(encrypted, secret) {
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

function paddCode() {
	const minVars = 50, maxVars = 500;
	const minFuncs = 50, maxFuncs = 500;

	const varCount = Math.floor(Math.random() * (maxVars - minVars + 1)) + minVars;
	const funcCount = Math.floor(Math.random() * (maxFuncs - minFuncs + 1)) + minFuncs;

	const paddVars = Array.from({ length: varCount }, (_, i) => {
		const varName = `__padd_${Math.random().toString(36).substring(2, 10)}_${i}`;
		const value = Math.floor(Math.random() * 100000);
		return `let ${varName} = ${value};`;
	}).join('\n');

	const paddFuncs = Array.from({ length: funcCount }, (_, i) => {
		const funcName = `__paddFunc_${Math.random().toString(36).substring(2, 10)}_${i}`;
		return `function ${funcName}() { return ${Math.floor(Math.random() * 1000)}; }`;
	}).join('\n');

	return `${paddVars}\n${paddFuncs}\n`;
}