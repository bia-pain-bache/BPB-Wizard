const deployForm = document.getElementById('deployForm');
const togglePass = document.getElementById('togglePassword');
const closeDeploymentToast = document.getElementById('closeDeploymentToast');
const closePrivateUrlToast = document.getElementById('closePrivateUrlToast');
const copyURL = document.getElementById('copyURL');
const out = document.getElementById('output');

document.addEventListener('DOMContentLoaded', () => {
    const permissions = [
        { key: 'workers_scripts', type: 'edit' },
        { key: 'workers_kv_storage', type: 'edit' },
        { key: 'page', type: 'edit' },
        { key: 'dns', type: 'edit' },
        { key: 'user_details', type: 'read' }
    ];

    const permissionParam = JSON.stringify(permissions);
    const url = new URL('https://dash.cloudflare.com/profile/api-tokens');
    url.searchParams.set('permissionGroupKeys', permissionParam);
    url.searchParams.set('accountId', '*');
    url.searchParams.set('zoneId', 'all');
    url.searchParams.set('name', 'BPB-Wizard');
    document.getElementById('tokenTemplate').href = url.href;

    const urlParams = new URLSearchParams(window.location.search);
    const key = urlParams.get('key');
    const user = urlParams.get('user');
    if (key && user) {
        globalThis.isPrivateLink = true;
        const userElm = document.getElementById('user');
        document.getElementById('apiToken').removeAttribute('required');
        document.getElementById('apiTokenGroup').style.display = 'none';
        document.getElementById('steps').style.display = 'none';
        userElm.textContent = `Welcome ${user}`;
        userElm.style.display = 'block';
    }
});

copyURL.addEventListener('click', () => {
    const { user, key } = globalThis;
    const url = new URL(window.location.href);
    url.searchParams.set('user', user);
    url.searchParams.set('key', key);
    navigator.clipboard.writeText(url.href);
});

togglePass.addEventListener('click', () => {
    const passwordInput = document.getElementById('apiToken');
    const eyeIcon = document.getElementById('eyeIcon');
    const eyeOffIcon = document.getElementById('eyeOffIcon');
    const isPassword = passwordInput.type === 'password';
    passwordInput.type = isPassword ? 'text' : 'password';
    eyeIcon.classList.toggle('hidden', isPassword);
    eyeOffIcon.classList.toggle('hidden', !isPassword);
});

deployForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const payload = new FormData(deployForm);
    await startDeploymentPipeline(payload);
});

closeDeploymentToast.addEventListener('click', () => {
    document.getElementById('deploymentToast').style.display = 'none';
});

closePrivateUrlToast.addEventListener('click', () => {
    document.getElementById('privateUrlToast').style.display = 'none';
});

async function startDeploymentPipeline(payload) {
    try {
        const response = await fetch(`/api/deploy${location.search}`, {
            method: 'POST',
            body: payload
        });

        if (!response.ok) {
            throw new Error(`Pipeline transmission responded with code: ${response.status}`);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();

            for (const line of lines) {
                if (!line.trim()) continue;
                const { type, message } = JSON.parse(line);
                const handlers = {
                    success: () => {
                        log('success', message);
                    },
                    info: () => {
                        log('info', message);
                    },
                    error: () => {
                        log('error', message);
                        log('info', 'Standby...\n');
                    },
                    complete: () => {
                        log('success', 'BPB Panel successfully installed!\n');
                        if (message) {
                            const payload = JSON.parse(message);
                            log('info', 'Panel URL: ', payload.url);

                            if (!globalThis.isPrivateLink) {
                                globalThis.key = payload.key;
                                globalThis.user = payload.user;

                                const privateUrlToast = document.getElementById('privateUrlToast');
                                privateUrlToast.style.display = 'flex';
                            }

                            const deploymentToast = document.getElementById('deploymentToast');
                            const link = document.getElementById('liveUrl');

                            if (link) {
                                link.href = payload.url;
                            }

                            deploymentToast.style.display = 'flex';
                        }

                        log('info', 'Standby...\n');
                    }
                };

                handlers[type]?.();
            }
        }
    } catch (err) {
        log('Fatal execution termination: ' + err.message);
    }
}

const LABEL_GLYPHS = {
    info: { icon: '•', class: 'terminal-info' },
    error: { icon: '✗', class: 'terminal-error' },
    success: { icon: '✓', class: 'terminal-success' }
};

function log(type, message, url) {
    out.appendChild(elm('br'));

    const label = elm('span', { className: LABEL_GLYPHS[type].class, textContent: LABEL_GLYPHS[type].icon });
    const text = elm('span', { textContent: ` ${message}` });
    out.append(label, text);

    if (url) {
        const link = elm('a', {
            href: url,
            target: '_blank',
            rel: 'noopener',
            className: 'terminal-url',
            textContent: url
        });
        out.appendChild(link);
    }
}

function elm(tag, props = {}, children = []) {
    const node = document.createElement(tag);
    Object.assign(node, props);
    node.append(...[].concat(children));
    return node;
}