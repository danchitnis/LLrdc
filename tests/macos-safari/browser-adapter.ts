import { chromium, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { Builder, By, Key, type WebDriver, type WebElement } from 'selenium-webdriver';
import * as fs from 'node:fs/promises';

export type BrowserName = 'chrome' | 'safari';

export interface BrowserAdapter {
    readonly name: BrowserName;
    start(): Promise<void>;
    goto(url: string): Promise<void>;
    evaluate<T>(script: string, ...args: unknown[]): Promise<T>;
    click(selector: string): Promise<void>;
    select(selector: string, value: string): Promise<void>;
    text(selector: string): Promise<string>;
    waitFor(script: string, timeoutMs?: number, ...args: unknown[]): Promise<void>;
    windowSize(): Promise<{ width: number; height: number }>;
    setWindowSize(width: number, height: number): Promise<void>;
    pointerMove(selector: string, x: number, y: number): Promise<void>;
    pointerClick(selector: string, x?: number, y?: number): Promise<void>;
    typeText(text: string): Promise<void>;
    keyCombo(modifier: 'Control' | 'Meta', key: string): Promise<void>;
    screenshot(path: string): Promise<void>;
    close(): Promise<void>;
}

const CHROME_PATH = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

function assertChromeIdentity(executablePath: string, version: string, userAgent: string): void {
    if (executablePath !== CHROME_PATH) {
        throw new Error(`Refusing non-installed Chrome executable: ${executablePath}`);
    }
    if (/chromium|headlesschrome/i.test(`${version} ${userAgent}`)) {
        throw new Error(`Bundled Chromium/headless Chrome detected: ${version} ${userAgent}`);
    }
    if (!/Chrome\//i.test(userAgent)) {
        throw new Error(`Installed Chrome identity was not reported by the browser: ${userAgent}`);
    }
}

export class ChromeAdapter implements BrowserAdapter {
    readonly name = 'chrome' as const;
    private browser?: Browser;
    private context?: BrowserContext;
    private page?: Page;
    private cdp?: Awaited<ReturnType<BrowserContext['newCDPSession']>>;

    async start(): Promise<void> {
        if (!(await fs.stat(CHROME_PATH).catch(() => undefined))) {
            throw new Error(`Installed Chrome was not found at ${CHROME_PATH}`);
        }
        this.browser = await chromium.launch({
            executablePath: CHROME_PATH,
            headless: false,
            args: ['--autoplay-policy=no-user-gesture-required', '--window-size=1324,931'],
        });
        this.context = await this.browser.newContext({ viewport: null });
        this.page = await this.context.newPage();
        this.cdp = await this.context.newCDPSession(this.page);
        const identity = await this.evaluate<{ version: string; userAgent: string }>(
            '() => ({ version: navigator.userAgentData?.brands?.map((b) => b.brand + "/" + b.version).join(" ") || navigator.userAgent, userAgent: navigator.userAgent })',
        );
        assertChromeIdentity(CHROME_PATH, identity.version, identity.userAgent);
    }

    private getPage(): Page {
        if (!this.page) throw new Error('Chrome adapter is not started');
        return this.page;
    }

    async goto(url: string): Promise<void> {
        await this.getPage().goto(url, { waitUntil: 'domcontentloaded' });
    }

    async evaluate<T>(script: string, ...args: unknown[]): Promise<T> {
        return await this.getPage().evaluate(({ script, args }) => {
            // The script is authored by this test suite, not supplied by a test subject.
            const fn = Function(`return (${script})`)();
            return typeof fn === 'function' ? fn(...args) : fn;
        }, { script, args }) as T;
    }

    async click(selector: string): Promise<void> {
        await this.getPage().locator(selector).click();
    }

    async select(selector: string, value: string): Promise<void> {
        await this.getPage().locator(selector).selectOption(value);
    }

    async text(selector: string): Promise<string> {
        return await this.getPage().locator(selector).innerText();
    }

    async waitFor(script: string, timeoutMs = 30000, ...args: unknown[]): Promise<void> {
        await this.getPage().waitForFunction(({ script, args }) => {
            const fn = Function(`return (${script})`)();
            return typeof fn === 'function' ? fn(...args) : fn;
        }, { script, args }, { timeout: timeoutMs });
    }

    async windowSize(): Promise<{ width: number; height: number }> {
        return await this.evaluate<{ width: number; height: number }>('() => ({ width: innerWidth, height: innerHeight })');
    }

    async setWindowSize(width: number, height: number): Promise<void> {
        if (!this.cdp) throw new Error('Chrome CDP session is not available');
        const info = await this.cdp.send('Browser.getWindowForTarget');
        await this.cdp.send('Browser.setWindowBounds', { windowId: info.windowId, bounds: { width, height, windowState: 'normal' } });
    }

    async pointerMove(selector: string, x: number, y: number): Promise<void> {
        const box = await this.getPage().locator(selector).boundingBox();
        if (!box) throw new Error(`Cannot find pointer target ${selector}`);
        await this.getPage().mouse.move(box.x + x, box.y + y);
    }

    async pointerClick(selector: string, x?: number, y?: number): Promise<void> {
        const box = await this.getPage().locator(selector).boundingBox();
        if (!box) throw new Error(`Cannot find pointer target ${selector}`);
        await this.getPage().mouse.click(box.x + (x ?? box.width / 2), box.y + (y ?? box.height / 2));
    }

    async typeText(text: string): Promise<void> {
        await this.getPage().keyboard.type(text);
    }

    async keyCombo(modifier: 'Control' | 'Meta', key: string): Promise<void> {
        await this.getPage().keyboard.press(`${modifier}+${key}`);
    }

    async screenshot(path: string): Promise<void> {
        await this.getPage().screenshot({ path, fullPage: true });
    }

    async close(): Promise<void> {
        await this.context?.close().catch(() => undefined);
        await this.browser?.close().catch(() => undefined);
        this.page = undefined;
        this.context = undefined;
        this.browser = undefined;
        this.cdp = undefined;
    }
}

export class SafariAdapter implements BrowserAdapter {
    readonly name = 'safari' as const;
    private driver?: WebDriver;

    async start(): Promise<void> {
        this.driver = await new Builder().forBrowser('safari').build();
        const capabilities = await this.driver.getCapabilities();
        const browserName = capabilities.get('browserName');
        if (String(browserName).toLowerCase() !== 'safari') {
            await this.close();
            throw new Error(`Safari WebDriver reported ${String(browserName)} instead of safari`);
        }
        const userAgent = await this.evaluate<string>('() => navigator.userAgent');
        if (!/Safari\//i.test(userAgent) || /Chrome|Chromium|Headless/i.test(userAgent)) {
            await this.close();
            throw new Error(`Installed Safari identity was not reported by safaridriver: ${userAgent}`);
        }
    }

    private getDriver(): WebDriver {
        if (!this.driver) throw new Error('Safari adapter is not started');
        return this.driver;
    }

    private async element(selector: string): Promise<WebElement> {
        return await this.getDriver().findElement(By.css(selector));
    }

    async goto(url: string): Promise<void> {
        try {
            await this.getDriver().get(url);
        } catch (error) {
            throw new Error(`Safari navigation failed for ${url}. Ensure Safari Develop > Allow Remote Automation is enabled: ${String(error)}`);
        }
    }

    async evaluate<T>(script: string, ...args: unknown[]): Promise<T> {
        return await this.getDriver().executeScript(`const fn = (${script}); return typeof fn === 'function' ? fn(...arguments) : fn;`, ...args) as T;
    }

    async click(selector: string): Promise<void> {
        try {
            await (await this.element(selector)).click();
        } catch (error) {
            if (!String(error).includes('ElementNotInteractable')) throw error;
            await this.evaluate<void>(((selectorArg: string) => {
                const element = document.querySelector(selectorArg) as HTMLElement | null;
                if (!element) throw new Error(`Missing clickable element ${selectorArg}`);
                element.click();
            }).toString(), selector);
        }
    }

    async select(selector: string, value: string): Promise<void> {
        await this.evaluate<void>(((selectorArg: string, valueArg: string) => {
            const element = document.querySelector(selectorArg as string) as HTMLSelectElement | null;
            if (!element) throw new Error(`Missing select ${selectorArg}`);
            element.value = valueArg as string;
            element.dispatchEvent(new Event('change', { bubbles: true }));
        }).toString(), selector, value);
    }

    async text(selector: string): Promise<string> {
        return await (await this.element(selector)).getText();
    }

    async waitFor(script: string, timeoutMs = 30000, ...args: unknown[]): Promise<void> {
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
            if (await this.evaluate<boolean>(script, ...args)) return;
            await new Promise(resolve => setTimeout(resolve, 200));
        }
        throw new Error(`Timed out after ${timeoutMs}ms waiting for Safari condition: ${script}`);
    }

    async windowSize(): Promise<{ width: number; height: number }> {
        return await this.evaluate<{ width: number; height: number }>('() => ({ width: innerWidth, height: innerHeight })');
    }

    async setWindowSize(width: number, height: number): Promise<void> {
        await this.getDriver().manage().window().setRect({ width, height });
    }

    async pointerMove(selector: string, x: number, y: number): Promise<void> {
        const point = await this.evaluate<{ left: number; top: number }>(
            ((selectorArg: string) => {
                const element = document.querySelector(selectorArg) as HTMLElement | null;
                if (!element) throw new Error(`Missing pointer target ${selectorArg}`);
                const rect = element.getBoundingClientRect();
                return { left: rect.left, top: rect.top };
            }).toString(),
            selector,
        );
        await this.getDriver().actions({ async: true }).move({ x: point.left + x, y: point.top + y }).perform();
    }

    async pointerClick(selector: string, x?: number, y?: number): Promise<void> {
        const point = await this.evaluate<{ left: number; top: number; width: number; height: number }>(
            ((selectorArg: string) => {
                const element = document.querySelector(selectorArg) as HTMLElement | null;
                if (!element) throw new Error(`Missing pointer target ${selectorArg}`);
                const rect = element.getBoundingClientRect();
                return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
            }).toString(),
            selector,
        );
        await this.getDriver().actions({ async: true }).move({ x: point.left + (x ?? point.width / 2), y: point.top + (y ?? point.height / 2) }).press().release().perform();
    }

    async typeText(text: string): Promise<void> {
        await this.getDriver().actions({ async: true }).sendKeys(text).perform();
    }

    async keyCombo(modifier: 'Control' | 'Meta', key: string): Promise<void> {
        const modifierKey = modifier === 'Meta' ? Key.COMMAND : Key.CONTROL;
        await this.getDriver().actions({ async: true }).keyDown(modifierKey).sendKeys(key).keyUp(modifierKey).perform();
    }

    async screenshot(path: string): Promise<void> {
        const image = await this.getDriver().takeScreenshot();
        await fs.writeFile(path, image, 'base64');
    }

    async close(): Promise<void> {
        await this.driver?.quit().catch(() => undefined);
        this.driver = undefined;
    }
}

export function createBrowserAdapter(name: BrowserName): BrowserAdapter {
    return name === 'chrome' ? new ChromeAdapter() : new SafariAdapter();
}
