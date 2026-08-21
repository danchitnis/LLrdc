import { WebCodecsManager } from '../webcodecs';
import { WebTransportManager } from '../webtransport';
import { WebSocketTransport } from '../websocket-transport';
import { BrowserClientApi, PresentedFrameMeta, BrowserStats, ConfigMessage } from './types';

declare global {
    interface Window {
        getStats: () => BrowserStats;
        hasReceivedKeyFrame: boolean;
        hardwareAccelerationAvailable: boolean;
        serverFfmpegFps?: number;
        webcodecsManager: WebCodecsManager;
        webtransportManager: WebTransportManager;
        websocketTransport: WebSocketTransport;
        __llrdcClient?: BrowserClientApi;
        __llrdcLatestFrameMeta?: PresentedFrameMeta;
        sendConfig: () => void;
        buildConfigMessage: () => ConfigMessage;
    }
}

export {};
