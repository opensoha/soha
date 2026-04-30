import { message } from 'antd';

export const errorConfig = {
  errorConfig: {
    errorHandler: (error: any, opts: { skipErrorHandler?: boolean } | undefined) => {
      if (opts?.skipErrorHandler) {
        throw error;
      }

      const status = error?.response?.status;
      const data = error?.response?.data;
      const msg =
        data?.message ||
        (typeof data?.error === 'string' ? data.error : data?.error?.message) ||
        error?.message ||
        'Request failed';

      if (status === 401) {
        if (typeof window !== 'undefined') {
          const redirect = `${window.location.pathname}${window.location.search}${window.location.hash}`;
          window.location.href = `/login?redirect=${encodeURIComponent(redirect)}`;
          return;
        }
      }

      message.error(msg);
    },
  },
};
