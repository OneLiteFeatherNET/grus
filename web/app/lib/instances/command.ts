/**
 * Prepares a console command for the API: trimmed, and null when it is empty or spans several lines
 * (the controller refuses line breaks).
 */
export function normalizeCommand(input: string): string | null {
  const line = input.trim()
  if (!line || /[\r\n]/.test(line)) return null
  return line
}
