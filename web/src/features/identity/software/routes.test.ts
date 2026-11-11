import { describe, expect, it, vi } from 'vitest'
import { internalWorkbenchSoftwareRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ SoftwareLibraryPage: routePage }))

describe('internal workbench software route manifest', () => {
  it('registers the protected software library under the internal workbench', async () => {
    expect(internalWorkbenchSoftwareRoutes).toHaveLength(1)
    const [libraryRoute] = internalWorkbenchSoftwareRoutes
    expect(libraryRoute.meta).toEqual(
      expect.objectContaining({
        id: 'internal-workbench-software',
        path: '/internal-workbench/software',
        parentId: 'internal-workbench',
        menuId: 'identity-software',
        permissionKey: 'software.package.view',
      }),
    )
    await expect(libraryRoute.load()).resolves.toEqual({ default: routePage })
  })
})
