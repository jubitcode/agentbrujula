const push = jest.fn();
const replace = jest.fn();
const back = jest.fn();

export const useRouter = () => ({ push, replace, back });
export const usePathname = () => "/";
export const useSearchParams = () => new URLSearchParams();
