import { WebCodecsManager } from '../webcodecs';
import { WebTransportManager } from '../webtransport';
import { BrowserClientApi, PresentedFrameMeta, BrowserStats, ConfigMessage } from './types';

declare global {
    interface Window {
        getStats: () => BrowserStats;
        hasReceivedKeyFrame: boolean;
        rtcPeer: any;
        hardwareAccelerationAvailable: boolean;
        serverFfmpegFps?: number;
        webcodecsManager: WebCodecsManager;
        webtransportManager: WebTransportManager;
        __llrdcClient?: BrowserClientApi;
        __llrdcLatestFrameMeta?: PresentedFrameMeta;
        sendConfig: () => void;
        buildConfigMessage: () => ConfigMessage;
    }
}

export {};
