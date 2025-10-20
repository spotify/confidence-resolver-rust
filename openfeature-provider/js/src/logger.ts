const NOOP_LOG_FN = () => {};
export type LogFn = (msg: string, ...rest: any[]) => void;

type Debug = typeof import('debug')['default'];

const debugBackend = loadDebug();

export interface Logger {
  debug(msg: string, ...args: any[]): void;
  info(msg: string, ...args: any[]): void;
  warn(msg: string, ...args: any[]): void;
  error(msg: string, ...args: any[]): void;

  readonly name: string;

  getLogger(name: string): Logger;
}

class LoggerImpl implements Logger {
  private readonly childLoggers = new Map<string, LoggerImpl>();

  debug: LogFn = NOOP_LOG_FN;
  info: LogFn = NOOP_LOG_FN;
  warn: LogFn = NOOP_LOG_FN;
  error: LogFn = NOOP_LOG_FN;

  constructor(readonly name: string) {
    this.configure();
  }

  async configure() {
    // TODO we should queue messages logged before configure is done
    const debug = await debugBackend;
    if (!debug) return;
    const debugFn = (this.debug = debug(this.name + ':debug'));
    const infoFn = (this.info = debug(this.name + ':info'));
    const warnFn = (this.warn = debug(this.name + ':warn'));
    const errorFn = (this.error = debug(this.name + ':error'));

    switch (true) {
      case debugFn.enabled:
        infoFn.enabled = true;
      case infoFn.enabled:
        warnFn.enabled = true;
      case warnFn.enabled:
        errorFn.enabled = true;
    }
  }

  getLogger(name: string): Logger {
    let child = this.childLoggers.get(name);
    if (!child) {
      child = new LoggerImpl(this.name + ':' + name);
      this.childLoggers.set(name, child);
    }
    return child;
  }
}

export const logger = new LoggerImpl('cnfd');

async function loadDebug(): Promise<Debug | null> {
  try {
    const { default: debug } = await import('debug');
    return debug;
  } catch (e) {
    // debug not available
    return null;
  }
}
