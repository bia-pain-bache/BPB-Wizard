export function randString(charset: string, minLen: number, maxLen: number) {
    const length = Math.floor(Math.random() * (maxLen - minLen + 1)) + minLen;
    const arr = new Uint8Array(length);
    crypto.getRandomValues(arr);
    let string = '';

    for (let i = 0; i < length; i++) {
        string += charset[arr[i] % charset.length];
    }

    return string;
}

function randInt(min: number, max: number): number {
    const rand = Math.floor(Math.random() * (max - min + 1)) + min;
    return rand;
}

export function randSubdomain() {
    const charset = "abcdefghijklmnopqrstuvwxyz0123456789-";
    let subdomain: string;
    
    do {
        subdomain = randString(charset, 16, 32);
    } while (subdomain.startsWith('-') || subdomain.endsWith('-'));
    
    return subdomain;
}

export function randCode(): string {
    const minVars = 50, maxVars = 500;
    const minFuncs = 50, maxFuncs = 500;
    const charset = 'abcdefghijklmnopqrstuvwxyz0123456789';

    const varCount = randInt(minVars, maxVars);
    const funcCount = randInt(minFuncs, maxFuncs);

    let varsBuilder = '';
    for (let i = 0; i < varCount; i++) {
        const varName = `__var_${randString(charset, 8, 16)}_${i}`;
        const value = randInt(0, 99999);
        varsBuilder += `let ${varName} = ${value};\n`;
    }

    let funcsBuilder = '';
    for (let i = 0; i < funcCount; i++) {
        const funcName = `__func_${randString(charset, 8, 16)}_${i}`;
        const value = randInt(0, 999);
        funcsBuilder += `function ${funcName}() { return ${value}; }\n`;
    }

    return varsBuilder + funcsBuilder;
}