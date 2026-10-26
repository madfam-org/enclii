// This pure contract is also exercised offline; it never sends a request.
export function healthProfile(env, stages) {
  const profile = env.HEALTH_PROFILE || 'smoke';
  if (profile === 'smoke') {
    return { name: 'public-liveness-smoke', path: '/health/public', stages: stages.smoke };
  }
  if (profile !== 'deep-diagnostic' || env.ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC !== 'true') {
    throw new Error('Deep health diagnostics require HEALTH_PROFILE=deep-diagnostic and explicit ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC=true');
  }
  return { name: 'deep-health-diagnostic', path: '/health', stages: stages.load };
}

export function healthyResponse(response, deep = false) {
  if (response.status !== 200) return false;
  try {
    const body = JSON.parse(response.body);
    return body.service === 'switchyard-api' && (deep ? body.status === 'healthy' : body.ok === true);
  } catch {
    return false;
  }
}
