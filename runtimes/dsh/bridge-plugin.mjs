// Trusted, immutable Cordis plugin; credentials and routing stay in the parent.
export const name = 'soha-tool-bridge';
export const inject = ['tools'];
export const toolOutcomeHeader = 'x-soha-tool-outcome';
export const toolOutcomeMessage = Object.freeze({
  denied: 'Soha tool denied',
  unknown: 'Soha tool outcome unknown',
});
export async function apply(ctx, config) {
  for (const tool of config.tools)
    ctx.tools.register({
      name: tool.alias,
      description: tool.description,
      parameters: tool.sdkInputSchema ?? tool.inputSchema,
      output: {
        schema: {},
        render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }],
      },
      async execute(input, exec) {
        const response = await fetch(config.url, {
          method: 'POST',
          redirect: 'error',
          headers: {
            authorization: `Bearer ${process.env.SOHA_DSH_SYNTHETIC_KEY}`,
            'content-type': 'application/json',
          },
          body: JSON.stringify({ alias: tool.alias, callId: exec.callId, input }),
          signal: exec.signal,
        });
        if (!response.ok) {
          const outcome = response.headers.get(toolOutcomeHeader);
          const message = Object.hasOwn(toolOutcomeMessage, outcome)
            ? toolOutcomeMessage[outcome]
            : 'Soha tool unavailable';
          throw new Error(message);
        }
        return response.json();
      },
    });
  if (config.registrationURL) {
    const response = await fetch(config.registrationURL, {
      method: 'POST',
      redirect: 'error',
      headers: {
        authorization: `Bearer ${process.env.SOHA_DSH_SYNTHETIC_KEY}`,
        'content-type': 'application/json',
      },
      body: JSON.stringify(config.tools.map((tool) => tool.alias)),
      signal: AbortSignal.timeout(2000),
    });
    if (response.status !== 204) throw new Error('Soha tool registration rejected');
  }
}
