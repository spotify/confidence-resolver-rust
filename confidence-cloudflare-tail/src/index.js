export default {
    async tail(events, env, ctx) {
        // Retrieve the queue binding
        const queue = env.flag_logs_queue;
        if (!queue) {
            console.error("LOG_QUEUE binding is missing");
            return;
        }

        for (const event of events) {
            try {
                // Filter events from confidence-cloudflare-resolver only
                if (event.scriptName !== "confidence-cloudflare-resolver") {
                    continue;
                }

                // Process each log entry in the event
                for (const logEntry of event.logs || []) {
                    try {
                        // Check if the log message starts with FLAGS_LOGS_QUEUE:
                        const messageStr = Array.isArray(logEntry.message)
                            ? logEntry.message.join(' ')
                            : String(logEntry.message || '');

                        if (messageStr.startsWith('FLAGS_LOGS_QUEUE:')) {
                            // Remove the FLAGS_LOGS_QUEUE: prefix
                            const cleanedMessage = messageStr.substring('FLAGS_LOGS_QUEUE:'.length).trim();

                            try {
                                // Parse the JSON payload to validate it's valid JSON
                                JSON.parse(cleanedMessage);
                                await queue.send(cleanedMessage);
                            } catch (parseErr) {
                                console.error("Failed to parse JSON payload:", parseErr);
                                console.error("Raw message was:", cleanedMessage);
                            }
                        }
                    } catch (logErr) {
                        console.error("Failed to process log entry:", logErr);
                    }
                }
            } catch (err) {
                console.error("Failed to process event:", err);
            }
        }
    }
};
