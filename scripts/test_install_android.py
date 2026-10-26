#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install-android.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)

class IdentityTests(unittest.TestCase):
    def test_requires_an_identifier(self):
        self.assertFalse(installer.matches({}, 'serial-a', '00:11:22:33:44:55'))

    def test_both_identifiers_must_match(self):
        selector = {'serial_number': 'SERIAL-A', 'mac': '00:11:22:33:44:55'}
        self.assertTrue(installer.matches(selector, 'serial-a', '00-11-22-33-44-55'))
        self.assertFalse(installer.matches(selector, 'serial-b', '00:11:22:33:44:55'))
        self.assertFalse(installer.matches(selector, 'serial-a', '00:11:22:33:44:66'))

if __name__ == '__main__':
    unittest.main()
