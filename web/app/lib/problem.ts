// The RFC 9457 helpers live in Pavo. Re-exported so that existing imports keep working and both sides share the same
// error classes (instanceof checks in M3Problem and in the pages).
export {
  ApiError,
  NetworkError,
  problemFromBody,
  toClientError,
  toTransportError,
} from '@onelitefeathernet/pavo/problem'
export type { ProblemDetails, ProblemError, TransportData, TransportError } from '@onelitefeathernet/pavo/problem'
