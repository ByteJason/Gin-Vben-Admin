import { describe, expect, it, vi } from 'vitest';

import {
  authenticateThenNavigate,
  bootstrapAuthenticatedSession,
} from './auth-bootstrap';

interface FixtureUser {
  accessCodes?: string[];
  userId: string;
}

function setup(user: FixtureUser, legacyResult: string[] | Error = ['legacy']) {
  const stageAccessToken = vi.fn();
  const commit = vi.fn();
  const rollback = vi.fn();
  const fetchLegacyAccessCodes = vi.fn(async () => {
    if (legacyResult instanceof Error) throw legacyResult;
    return legacyResult;
  });

  return {
    commit,
    fetchLegacyAccessCodes,
    options: {
      accessToken: 'access-token',
      commit,
      fetchLegacyAccessCodes,
      fetchUserInfo: vi.fn(async () => user),
      rollback,
      stageAccessToken,
    },
    rollback,
    stageAccessToken,
  };
}

describe('bootstrapAuthenticatedSession', () => {
  it('commits accessCodes from /iam/me without calling the legacy endpoint', async () => {
    const fixture = setup({ accessCodes: ['iam:user:read'], userId: 'u1' });

    const user = await bootstrapAuthenticatedSession(fixture.options);

    expect(user.userId).toBe('u1');
    expect(fixture.stageAccessToken).toHaveBeenCalledOnce();
    expect(fixture.fetchLegacyAccessCodes).not.toHaveBeenCalled();
    expect(fixture.commit).toHaveBeenCalledWith({
      accessCodes: ['iam:user:read'],
      userInfo: user,
    });
    expect(fixture.rollback).not.toHaveBeenCalled();
  });

  it('treats an explicitly empty accessCodes array as authoritative', async () => {
    const fixture = setup({ accessCodes: [], userId: 'u1' });

    await bootstrapAuthenticatedSession(fixture.options);

    expect(fixture.fetchLegacyAccessCodes).not.toHaveBeenCalled();
    expect(fixture.commit).toHaveBeenCalledWith({
      accessCodes: [],
      userInfo: expect.objectContaining({ userId: 'u1' }),
    });
  });

  it('uses /auth/codes only for a legacy /iam/me payload', async () => {
    const fixture = setup({ userId: 'legacy-user' }, ['legacy:read']);

    await bootstrapAuthenticatedSession(fixture.options);

    expect(fixture.fetchLegacyAccessCodes).toHaveBeenCalledOnce();
    expect(fixture.commit).toHaveBeenCalledWith({
      accessCodes: ['legacy:read'],
      userInfo: expect.objectContaining({ userId: 'legacy-user' }),
    });
  });

  it('accepts the legacy endpoint specific 404 as an empty code set', async () => {
    const notFound = Object.assign(new Error('legacy endpoint missing'), {
      response: { status: 404 },
    });
    const fixture = setup({ userId: 'legacy-user' }, notFound);

    await bootstrapAuthenticatedSession(fixture.options);

    expect(fixture.commit).toHaveBeenCalledWith({
      accessCodes: [],
      userInfo: expect.objectContaining({ userId: 'legacy-user' }),
    });
    expect(fixture.rollback).not.toHaveBeenCalled();
  });

  it('rolls back only the staged authentication state on any other failure', async () => {
    const unavailable = Object.assign(new Error('upstream unavailable'), {
      response: { status: 503 },
    });
    const fixture = setup({ userId: 'legacy-user' }, unavailable);

    await expect(bootstrapAuthenticatedSession(fixture.options)).rejects.toBe(
      unavailable,
    );

    expect(fixture.commit).not.toHaveBeenCalled();
    expect(fixture.rollback).toHaveBeenCalledOnce();
  });

  it('waits for asynchronous session rollback and preserves the bootstrap error', async () => {
    const unavailable = Object.assign(new Error('upstream unavailable'), {
      response: { status: 503 },
    });
    const fixture = setup({ userId: 'legacy-user' }, unavailable);
    let finishRollback!: () => void;
    const rollbackFinished = new Promise<void>((resolve) => {
      finishRollback = resolve;
    });
    fixture.options.rollback = vi.fn(() => rollbackFinished);

    const bootstrap = bootstrapAuthenticatedSession(fixture.options);
    await vi.waitFor(() => expect(fixture.options.rollback).toHaveBeenCalled());

    const stateBeforeRollback = await Promise.race([
      bootstrap.then(
        () => 'resolved',
        () => 'rejected',
      ),
      Promise.resolve('pending'),
    ]);
    expect(stateBeforeRollback).toBe('pending');

    finishRollback();
    await expect(bootstrap).rejects.toBe(unavailable);
  });
});

describe('authenticateThenNavigate', () => {
  it('does not relabel a post-authentication navigation failure as login failure', async () => {
    const navigationError = new Error('router rejected navigation');
    const onAuthenticationFailure = vi.fn();
    const authenticatedState = { committed: true };

    await expect(
      authenticateThenNavigate({
        authenticate: async () => authenticatedState,
        navigate: async () => {
          throw navigationError;
        },
        onAuthenticationFailure,
      }),
    ).rejects.toBe(navigationError);

    expect(authenticatedState.committed).toBe(true);
    expect(onAuthenticationFailure).not.toHaveBeenCalled();
  });
});
