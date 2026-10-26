import unittest
from codex_voice_worker import CodexVoice


class VoiceWorkerTests(unittest.TestCase):
    def make_client(self, status="completed", model="gpt-6-luna"):
        client = CodexVoice.__new__(CodexVoice)
        client.pending = []
        client.sequence = 10
        client.config = {}
        client.send = lambda message: None
        client.rpc = lambda method, params, deadline: (
            {"thread": {"id": "target"}, "model": model} if method == "thread/start"
            else {"turn": {"id": "turn"}})
        events = iter([
            {"method": "item/agentMessage/delta", "params": {"threadId": "other", "delta": "wrong"}},
            {"method": "item/agentMessage/delta", "params": {"threadId": "target", "delta": "回答"}},
            {"method": "turn/completed", "params": {"threadId": "target", "turn": {"status": status}}},
        ])
        client.receive = lambda deadline: next(events)
        return client

    def test_only_completed_target_turn_is_returned(self):
        self.assertEqual(self.make_client().answer("问题"), "回答")

    def test_failed_turn_does_not_speak_partial_text(self):
        with self.assertRaises(RuntimeError):
            self.make_client(status="failed").answer("问题")

    def test_model_substitution_is_rejected(self):
        with self.assertRaises(RuntimeError):
            self.make_client(model="other-model").answer("问题")

    def test_unbounded_input_rejected(self):
        with self.assertRaises(ValueError):
            self.make_client().answer("字" * 2001)


if __name__ == "__main__":
    unittest.main()
