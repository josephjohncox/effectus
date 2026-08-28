import * as fs from 'fs';
import * as path from 'path';
import { execFile } from 'child_process';

export interface EffectuscResolverOptions {
    configuredPath?: string;
    workspaceFolder?: string;
    env?: NodeJS.ProcessEnv;
    platform?: NodeJS.Platform;
}

export interface EffectuscResolution {
    path?: string;
    source?: 'setting' | 'workspace' | 'path' | 'gopath' | 'home';
    error?: string;
}

export interface EffectuscRunResult {
    stdout: string;
    stderr: string;
    exitCode: number;
}

function executableExtensions(platform: NodeJS.Platform, env: NodeJS.ProcessEnv): string[] {
    if (platform !== 'win32') {
        return [''];
    }
    const extensions = (env.PATHEXT || '.COM;.EXE;.BAT;.CMD')
        .split(';')
        .filter(Boolean)
        .map(extension => extension.toLowerCase());
    return ['', ...extensions];
}

function isExecutable(candidate: string, platform: NodeJS.Platform): boolean {
    try {
        const stat = fs.statSync(candidate);
        if (!stat.isFile()) {
            return false;
        }
        if (platform === 'win32') {
            return true;
        }
        fs.accessSync(candidate, fs.constants.X_OK);
        return true;
    } catch {
        return false;
    }
}

function findCandidate(base: string, platform: NodeJS.Platform, env: NodeJS.ProcessEnv): string | undefined {
    const hasExtension = path.extname(base) !== '';
    const extensions = hasExtension ? [''] : executableExtensions(platform, env);
    for (const extension of extensions) {
        const candidate = base + extension;
        if (isExecutable(candidate, platform)) {
            return candidate;
        }
    }
    return undefined;
}

/** Resolve effectusc without invoking a shell. An explicit setting never silently falls back. */
export function resolveEffectusc(options: EffectuscResolverOptions = {}): EffectuscResolution {
    const env = options.env || process.env;
    const platform = options.platform || process.platform;
    const configuredPath = options.configuredPath?.trim();

    if (configuredPath) {
        const expanded = path.isAbsolute(configuredPath)
            ? configuredPath
            : path.resolve(options.workspaceFolder || process.cwd(), configuredPath);
        const resolved = findCandidate(expanded, platform, env);
        if (resolved) {
            return { path: resolved, source: 'setting' };
        }
        return {
            error: `The configured effectus.lsp.serverPath is not an executable file: ${expanded}`
        };
    }

    if (options.workspaceFolder) {
        const resolved = findCandidate(path.join(options.workspaceFolder, 'bin', 'effectusc'), platform, env);
        if (resolved) {
            return { path: resolved, source: 'workspace' };
        }
    }

    for (const entry of (env.PATH || '').split(path.delimiter).filter(Boolean)) {
        const resolved = findCandidate(path.join(entry, 'effectusc'), platform, env);
        if (resolved) {
            return { path: resolved, source: 'path' };
        }
    }

    if (env.GOPATH) {
        for (const goPath of env.GOPATH.split(path.delimiter).filter(Boolean)) {
            const resolved = findCandidate(path.join(goPath, 'bin', 'effectusc'), platform, env);
            if (resolved) {
                return { path: resolved, source: 'gopath' };
            }
        }
    }

    if (env.HOME) {
        const resolved = findCandidate(path.join(env.HOME, 'go', 'bin', 'effectusc'), platform, env);
        if (resolved) {
            return { path: resolved, source: 'home' };
        }
    }

    return { error: 'effectusc was not found on PATH or in effectus.lsp.serverPath.' };
}

export function runEffectusc(
    executable: string,
    args: readonly string[],
    options: { cwd?: string; timeout?: number; maxBuffer?: number } = {}
): Promise<EffectuscRunResult> {
    return new Promise((resolve, reject) => {
        execFile(executable, [...args], {
            cwd: options.cwd,
            timeout: options.timeout ?? 15_000,
            maxBuffer: options.maxBuffer ?? 4 * 1024 * 1024,
            windowsHide: true
        }, (error, stdout, stderr) => {
            if (!error) {
                resolve({ stdout, stderr, exitCode: 0 });
                return;
            }
            if (typeof error.code === 'number') {
                resolve({ stdout, stderr, exitCode: error.code });
                return;
            }
            reject(new Error(`Could not run effectusc: ${error.message}`));
        });
    });
}
