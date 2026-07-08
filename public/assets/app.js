const deployForm = document.getElementById('deployForm');
const togglePass = document.getElementById('togglePassword');
const closeDeploymentToast = document.getElementById('closeDeploymentToast');
const closePrivateUrlToast = document.getElementById('closePrivateUrlToast');
const copyURL = document.getElementById('copyURL');
const out = document.getElementById("output");

document.addEventListener("DOMContentLoaded", () => {
    const urlParams = new URLSearchParams(window.location.search);
    const key = urlParams.get("key");
    const user = urlParams.get("user");
    if (key && user) {
        globalThis.isPrivateLink = true;
        const userElm = document.getElementById("user");
        document.getElementById("apiToken").removeAttribute("required");
        document.getElementById("apiTokenGroup").style.display = "none";
        document.getElementById("steps").style.display = "none";
        userElm.textContent = user;
        userElm.style.display = "block";
    }
});

copyURL.addEventListener('click', () => {
    const { user, key } = globalThis;
    const url = new URL(window.location.href);
    url.searchParams.set("user", user);
    url.searchParams.set("key", key);
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
            method: "POST",
            body: payload
        });

        if (!response.ok) {
            throw new Error(`Pipeline transmission responded with code: ${response.status}`);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split("\n");
            buffer = lines.pop();

            for (const line of lines) {
                if (!line.trim()) continue;
                const { type, message } = JSON.parse(line);
                const handlers = {
                    log: () => {
                        log("info", message);
                    },
                    error: () => {
                        log("error", message);
                        log("info", "Standby...\n");
                    },
                    complete: () => {
                        log("success", "BPB Panel successfully deployed!");
                        if (message) {
                            const payload = JSON.parse(message);
                            log("success", "Panel URL: ", payload.url);
                            if (!globalThis.isPrivateLink) {
                                globalThis.key = payload.key;
                                globalThis.user = payload.user;
                                const privateUrlToast = document.getElementById('privateUrlToast');
                                privateUrlToast.style.display = "flex";
                            }

                            const deploymentToast = document.getElementById('deploymentToast');
                            const link = document.getElementById('liveUrl');
                            if (link) link.href = payload.url;
                            deploymentToast.style.display = "flex";
                        }

                        log("info", "Standby...\n");
                    }
                };
                handlers[type]?.();
            }
        }
    } catch (err) {
        log("Fatal execution termination: " + err.message);
    }
}

function log(type, message, url) {
    const labels = {
        info: '<span class="terminal-info">[INFO]</span>',
        error: '<span class="terminal-error">[ERROR]</span>',
        success: '<span class="terminal-success">[SUCCESS]</span>'
    };

    let msg = `<br>${labels[type]} ${message}`;
    if (url) msg += `<a href="${url}" target="_blank" rel="noopener" class="terminal-url">${url}</a>`;
    out.innerHTML += msg;
}